from dataclasses import dataclass

from celery import shared_task
from fastapi import APIRouter, FastAPI
from flask import Flask

api = FastAPI()
router = APIRouter(prefix="/payments")
flask_app = Flask(__name__)

__all__ = ["PublicService"]


class BaseModel:
    @classmethod
    def from_dict(cls, obj: dict[str, str]):
        return cls(**obj)


@dataclass
class EventModel(BaseModel):
    name: str

    def __post_init__(self) -> None:
        self.name = str(self.name)

    @property
    def object_url(self) -> str:
        return f"s3://{self.name}"


class PublicService:
    def handle(self) -> str:
        return "public"


class LogProcessor:
    def create_partition(self, partition: "LogPartition") -> "LogPartition":
        return partition


class LogPartition:
    @staticmethod
    def from_event(source: dict[str, str], target: str) -> "LogPartition":
        return LogPartition()


def unused_helper() -> str:
    return "unused"


def direct_reference_target() -> str:
    return "referenced"


def direct_reference_caller() -> str:
    return direct_reference_target()


def model_reference_caller() -> str:
    event = EventModel.from_dict({"name": "logs"})
    return event.object_url


@api.get("/health")
async def fastapi_health() -> dict[str, bool]:
    return {"ok": True}


@flask_app.route("/status")
def flask_status() -> str:
    return "ok"


@shared_task
def celery_sync() -> str:
    return "ok"


def lambda_handler(event: dict[str, str], _context: object) -> dict[str, str]:
    log_processor = LogProcessor()
    partition = LogPartition.from_event(source=event, target="bucket")
    log_processor.create_partition(partition=partition)
    model_reference_caller()
    return {"status": "ok"}
