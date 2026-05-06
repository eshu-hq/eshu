import os


def database_host() -> str:
    return os.getenv("DATABASE_HOST", "shared-db.internal")
