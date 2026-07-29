from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles

from .database import init_db
from .routers import auth, profiles, sync, history, schedules, folders, batch, notifications, ws
from .services.job_completion import reconcile_orphan_jobs
from .services.scheduler import init_scheduler, scheduler


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup
    init_db()
    interrupted = reconcile_orphan_jobs()
    if interrupted:
        print(f"Marked {interrupted} job(s) left running by a previous shutdown as interrupted")
    init_scheduler()
    yield
    # Shutdown
    scheduler.shutdown(wait=False)


app = FastAPI(title="imapsync Web UI", version="1.0.0", lifespan=lifespan)

# CORS (dev mode - allow Vite dev server)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost:5173", "http://localhost:3000"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Routers
app.include_router(auth.router)
app.include_router(profiles.router)
app.include_router(sync.router)
app.include_router(history.router)
app.include_router(schedules.router)
app.include_router(folders.router)
app.include_router(batch.router)
app.include_router(notifications.router)
app.include_router(ws.router)

# Serve frontend static files in production
static_dir = Path(__file__).parent.parent.parent / "static"
if static_dir.exists():
    app.mount("/", StaticFiles(directory=str(static_dir), html=True), name="static")


# Health check
@app.get("/api/health")
def health():
    return {"status": "ok"}
