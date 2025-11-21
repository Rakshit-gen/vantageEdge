#!/usr/bin/env bash
# wait-for-it.sh: Wait for a service to be available

TIMEOUT=15
QUIET=0

usage() {
    cat << EOF
Usage: $0 host:port [-t timeout] [-- command args]
  -t TIMEOUT                  Timeout in seconds, zero for no timeout
  -q                          Quiet mode
  --                          Execute command with args after the test finishes
EOF
    exit 1
}

wait_for() {
    local host="$1"
    local port="$2"
    local start_ts=$(date +%s)
    
    while :; do
        if command -v nc >/dev/null 2>&1; then
            nc -z "$host" "$port" >/dev/null 2>&1
            result=$?
        else
            (echo > /dev/tcp/$host/$port) >/dev/null 2>&1
            result=$?
        fi
        
        if [[ $result -eq 0 ]]; then
            if [[ $QUIET -eq 0 ]]; then
                echo "Service $host:$port is available!"
            fi
            return 0
        fi
        
        local end_ts=$(date +%s)
        if [[ $((end_ts - start_ts)) -ge $TIMEOUT ]]; then
            echo "Timeout waiting for $host:$port"
            return 1
        fi
        
        sleep 1
    done
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        *:* )
            HOST=$(echo "$1" | cut -d: -f1)
            PORT=$(echo "$1" | cut -d: -f2)
            shift
            ;;
        -t)
            TIMEOUT="$2"
            shift 2
            ;;
        -q)
            QUIET=1
            shift
            ;;
        --)
            shift
            CLI=("$@")
            break
            ;;
        *)
            usage
            ;;
    esac
done

if [[ -z "$HOST" || -z "$PORT" ]]; then
    echo "Error: Host and port required"
    usage
fi

wait_for "$HOST" "$PORT"
RESULT=$?

if [[ ${#CLI[@]} -gt 0 ]]; then
    exec "${CLI[@]}"
fi

exit $RESULT
