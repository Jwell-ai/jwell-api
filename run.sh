#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

APP_NAME="${APP_NAME:-new-api}"
EXECUTABLE_PATH="${EXECUTABLE_PATH:-$SCRIPT_DIR/$APP_NAME}"
if [ ! -f "$EXECUTABLE_PATH" ] && [ -f "$SCRIPT_DIR/bin/$APP_NAME" ]; then
    EXECUTABLE_PATH="$SCRIPT_DIR/bin/$APP_NAME"
fi

PID_FILE="${PID_FILE:-$SCRIPT_DIR/$APP_NAME.pid}"
LOG_DIR="${LOG_DIR:-$SCRIPT_DIR/logs}"
APP_LOG="${APP_LOG:-$LOG_DIR/$APP_NAME.out.log}"
EXTRA_ARGS="${EXTRA_ARGS:-}"

show_help() {
    echo "Usage: $0 {start|stop|restart|status|tail|build|build-all|build-frontend|update|update-all|help}"
    echo ""
    echo "Commands:"
    echo "  start    - 启动服务"
    echo "  stop     - 停止服务"
    echo "  restart  - 重启服务"
    echo "  status   - 查看服务状态"
    echo "  tail     - 查看启动输出日志"
    echo "  build    - 执行 make build-backend，仅构建后端二进制"
    echo "  build-all - 执行 make all，构建前端并嵌入后端二进制"
    echo "  build-frontend - 执行 make build-frontend，仅构建前端"
    echo "  update   - 先停止，执行 make build-backend，再启动"
    echo "  update-all - 先停止，执行 make all，再启动"
    echo "  help     - 显示帮助信息"
    echo ""
    echo "Environment overrides:"
    echo "  APP_NAME=new-api"
    echo "  EXECUTABLE_PATH=$SCRIPT_DIR/new-api"
    echo "  PID_FILE=$SCRIPT_DIR/new-api.pid"
    echo "  LOG_DIR=$SCRIPT_DIR/logs"
    echo "  APP_LOG=$SCRIPT_DIR/logs/new-api.out.log"
    echo "  EXTRA_ARGS='--port 3001'"
    echo ""
    echo "Notes:"
    echo "  - .env 放在项目根目录即可，程序启动时会自动读取。"
    echo "  - master/slave 通过 .env 中的 NODE_TYPE=master|slave 指定。"
}

check_executable() {
    if [ ! -f "$EXECUTABLE_PATH" ]; then
        echo "Error: 可执行文件不存在: $EXECUTABLE_PATH"
        echo "请先执行: make build-backend，或需要单体部署时执行 make all"
        exit 1
    fi
    if [ ! -x "$EXECUTABLE_PATH" ]; then
        chmod +x "$EXECUTABLE_PATH"
    fi
}

get_pid() {
    if [ -f "$PID_FILE" ]; then
        local pid
        pid="$(cat "$PID_FILE" 2>/dev/null || true)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            echo "$pid"
            return 0
        fi
    fi

    pgrep -f "^$EXECUTABLE_PATH( |$)" 2>/dev/null | head -1 || true
}

is_running() {
    local pid
    pid="$(get_pid)"
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

start_service() {
    check_executable

    if is_running; then
        echo "服务已经在运行中 (PID: $(get_pid))"
        return 0
    fi

    mkdir -p "$LOG_DIR"
    cd "$SCRIPT_DIR"

    echo "正在启动服务..."
    echo "可执行文件: $EXECUTABLE_PATH"
    echo "启动日志: $APP_LOG"

    # shellcheck disable=SC2086
    nohup "$EXECUTABLE_PATH" --log-dir "$LOG_DIR" $EXTRA_ARGS > "$APP_LOG" 2>&1 &
    local pid=$!
    echo "$pid" > "$PID_FILE"

    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
        echo "服务启动成功 (PID: $pid)"
        return 0
    fi

    echo "服务启动失败，请检查日志: $APP_LOG"
    rm -f "$PID_FILE"
    return 1
}

stop_service() {
    local pid
    pid="$(get_pid)"

    if [ -z "$pid" ]; then
        echo "服务未运行"
        rm -f "$PID_FILE"
        return 0
    fi

    echo "正在停止服务 (PID: $pid)..."
    kill "$pid" 2>/dev/null || true

    local count=0
    while kill -0 "$pid" 2>/dev/null && [ "$count" -lt 10 ]; do
        sleep 1
        count=$((count + 1))
    done

    if kill -0 "$pid" 2>/dev/null; then
        echo "服务未响应，强制终止..."
        kill -9 "$pid" 2>/dev/null || true
    fi

    rm -f "$PID_FILE"
    echo "服务已停止"
}

show_status() {
    if is_running; then
        local pid
        pid="$(get_pid)"
        echo "服务正在运行 (PID: $pid)"
        ps -p "$pid" -o pid,ppid,cmd,%cpu,%mem 2>/dev/null || true
    else
        echo "服务未运行"
        [ -f "$PID_FILE" ] && echo "发现过期 PID 文件: $PID_FILE"
    fi
    return 0
}

build_service() {
    echo "开始构建服务..."
    cd "$SCRIPT_DIR"
    make build-backend
}

build_all_service() {
    echo "开始构建前端和嵌入式后端..."
    cd "$SCRIPT_DIR"
    make all
}

build_frontend() {
    echo "开始构建前端..."
    cd "$SCRIPT_DIR"
    make build-frontend
}

update_service() {
    echo "开始更新服务..."
    stop_service
    build_service
    start_service
}

update_all_service() {
    echo "开始更新前端和嵌入式后端..."
    stop_service
    build_all_service
    start_service
}

tail_log() {
    mkdir -p "$LOG_DIR"
    touch "$APP_LOG"
    tail -f "$APP_LOG"
}

case "${1:-help}" in
    start)
        start_service
        ;;
    stop)
        stop_service
        ;;
    restart)
        stop_service
        sleep 1
        start_service
        ;;
    status)
        show_status
        ;;
    tail)
        tail_log
        ;;
    build)
        build_service
        ;;
    build-all)
        build_all_service
        ;;
    build-frontend)
        build_frontend
        ;;
    update)
        update_service
        ;;
    update-all)
        update_all_service
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo "未知参数: $1"
        show_help
        exit 1
        ;;
esac
