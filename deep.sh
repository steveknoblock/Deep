#!/usr/bin/env bash
# deep.sh: start, stop, restart and check the Deep server.
# Lives in the Deep project directory. The server runs as "deep-server", so it
# is easy to spot in ps/top.
#
#   ./deep.sh build     compile ./server into ./deep-server
#   ./deep.sh start     run it in the background (rebuilds first if the code changed)
#   ./deep.sh start -f  same, but first stop whatever is holding Deep's port
#   ./deep.sh stop      stop it (SIGTERM, then wait)
#   ./deep.sh restart   stop, free the port if needed, start
#   ./deep.sh status
#   ./deep.sh logs      follow the log (Ctrl-C to leave)
#
# Settings (DEEP_PORT, DEEP_UI, HATCHECK_URL) are read from .env if present.
set -u

cd "$(dirname "$(readlink -f "$0")")" || exit 1
bin=./deep-server
pidfile=.deep.pid
logfile=deep.log
name=deep-server

if [[ -f .env ]]; then set -a; source .env; set +a; fi
port=${DEEP_PORT:-8091}

# PID of the running server, or nothing. Uses the pid file, falling back to
# a process-name search so a server started by hand is still found.
running_pid() {
  local pid
  if [[ -f $pidfile ]]; then
    pid=$(<"$pidfile")
    if [[ -n $pid ]] && [[ $(ps -p "$pid" -o comm= 2>/dev/null) == "$name" ]]; then
      echo "$pid"; return 0
    fi
  fi
  pgrep -x "$name" | head -n1
}

# "pid name" of whatever is listening on the port, or nothing.
# Uses ss (standard on Ubuntu), falling back to lsof.
port_owner() {
  local pid pname
  if command -v ss >/dev/null 2>&1; then
    pid=$(ss -H -ltnp "sport = :$port" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -n1)
  elif command -v lsof >/dev/null 2>&1; then
    pid=$(lsof -t -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | head -n1)
  fi
  [[ -n ${pid:-} ]] || return 0
  pname=$(ps -p "$pid" -o comm= 2>/dev/null)
  echo "$pid $pname"
}

# Stop whatever listens on the port. Returns 0 once the port is free.
free_port() {
  local owner pid pname
  owner=$(port_owner)
  [[ -z $owner ]] && return 0
  read -r pid pname <<<"$owner"
  echo "Port $port is held by '$pname' (pid $pid); stopping it ..."
  kill "$pid" 2>/dev/null
  for _ in $(seq 1 50); do
    [[ -z $(port_owner) ]] && return 0
    sleep 0.2
  done
  echo "Port $port is still in use. Force it with: kill -9 $pid" >&2
  return 1
}

do_build() {
  echo "Building $bin ..."
  go build -o "$bin" ./server/
}

# True if the binary is missing or any Go source is newer than it.
needs_build() {
  [[ -x $bin ]] || return 0
  [[ -n $(find . -name '*.go' -newer "$bin" -print -quit) || go.mod -nt $bin ]]
}

do_start() {
  local force=${1:-0} pid; pid=$(running_pid)
  if [[ -n $pid ]]; then echo "Already running (pid $pid)."; return 0; fi
  local owner opid opname
  owner=$(port_owner)
  if [[ -n $owner ]]; then
    read -r opid opname <<<"$owner"
    if [[ $force == 1 ]]; then
      free_port || return 1
    else
      echo "Port $port is already in use by '$opname' (pid $opid)." >&2
      echo "Run './deep.sh start -f' to stop it and start Deep." >&2
      return 1
    fi
  fi
  if needs_build; then do_build || return 1; fi
  setsid nohup "$bin" >>"$logfile" 2>&1 < /dev/null &
  echo $! > "$pidfile"
  sleep 1
  pid=$(running_pid)
  if [[ -n $pid ]]; then
    echo "Started (pid $pid). Log: $PWD/$logfile"
    echo "UI: http://localhost:$port/ui/notebook.html"
  else
    echo "Failed to start. Last log lines:" >&2
    tail -n 10 "$logfile" >&2
    rm -f "$pidfile"
    return 1
  fi
}

do_stop() {
  local pid; pid=$(running_pid)
  if [[ -z $pid ]]; then echo "Not running."; rm -f "$pidfile"; return 0; fi
  kill "$pid"
  for _ in $(seq 1 50); do          # wait up to 10 seconds
    kill -0 "$pid" 2>/dev/null || { echo "Stopped."; rm -f "$pidfile"; return 0; }
    sleep 0.2
  done
  echo "Still running after 10s (pid $pid). Force it with: kill -9 $pid" >&2
  return 1
}

do_status() {
  local pid; pid=$(running_pid)
  if [[ -n $pid ]]; then echo "Running (pid $pid)."; else echo "Not running."; return 1; fi
}

case "${1:-}" in
  build)   do_build ;;
  start)   if [[ ${2:-} == -f ]]; then do_start 1; else do_start 0; fi ;;
  stop)    do_stop ;;
  restart) do_stop && do_start 1 ;;
  status)  do_status ;;
  logs)    tail -n 50 -f "$logfile" ;;
  *)       echo "Usage: $0 {build|start|stop|restart|status|logs}" >&2; exit 2 ;;
esac
