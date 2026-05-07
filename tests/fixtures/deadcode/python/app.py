from celery import shared_task
from fastapi import APIRouter, FastAPI
from flask import Flask

api = FastAPI()
router = APIRouter(prefix="/payments")
flask_app = Flask(__name__)


class PublicService:
    def handle(self) -> str:
        return "public"


def unused_helper() -> str:
    return "unused"


def direct_reference_target() -> str:
    return "referenced"


def direct_reference_caller() -> str:
    return direct_reference_target()


@api.get("/health")
async def fastapi_health() -> dict[str, bool]:
    return {"ok": True}


@flask_app.route("/status")
def flask_status() -> str:
    return "ok"


@shared_task
def celery_sync() -> str:
    return "ok"
