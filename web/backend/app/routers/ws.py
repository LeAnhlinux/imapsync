from fastapi import APIRouter, WebSocket, WebSocketDisconnect, Query
import json

from ..security import decode_access_token
from ..services.imapsync_runner import get_running_job

router = APIRouter(prefix="/api/ws", tags=["websocket"])


@router.websocket("/jobs/{job_id}")
async def job_websocket(websocket: WebSocket, job_id: int, token: str = Query(...)):
    # Authenticate via token query param
    payload = decode_access_token(token)
    if not payload:
        await websocket.close(code=4001, reason="Invalid token")
        return

    await websocket.accept()

    running_job = get_running_job(job_id)
    if not running_job:
        await websocket.send_json({"type": "status", "status": "not_found"})
        await websocket.close()
        return

    # Send existing log lines
    for line in running_job.log_lines:
        await websocket.send_json({"type": "log", "data": line})

    # Send current progress if available
    if running_job.progress:
        await websocket.send_json({"type": "progress", **running_job.progress})

    # Subscribe to new messages
    async def send_message(msg: dict):
        try:
            await websocket.send_json(msg)
        except Exception:
            pass

    running_job.subscribers.append(send_message)

    try:
        # Keep connection alive until client disconnects
        while True:
            await websocket.receive_text()
    except WebSocketDisconnect:
        pass
    finally:
        if send_message in running_job.subscribers:
            running_job.subscribers.remove(send_message)
