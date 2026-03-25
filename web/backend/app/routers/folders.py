from fastapi import APIRouter, Depends

from ..deps import get_current_user
from ..models import User
from ..schemas import FolderBrowseRequest, FolderInfo
from ..services.folder_browser import browse_folders

router = APIRouter(prefix="/api/folders", tags=["folders"])


@router.post("/browse", response_model=list[FolderInfo])
async def browse(data: FolderBrowseRequest, user: User = Depends(get_current_user)):
    folders = await browse_folders(
        host=data.host, user=data.user, password=data.password, ssl=data.ssl
    )
    return folders
