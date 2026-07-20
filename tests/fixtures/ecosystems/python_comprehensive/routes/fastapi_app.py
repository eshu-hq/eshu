# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

# #5361 route query-proof matrix: cribs the FastAPI decorator shape proven by
# internal/parser/engine_python_semantics_test.go
# (TestDefaultEngineParsePathPythonFastAPISemantics).
from fastapi import APIRouter, FastAPI, Request

app: FastAPI = FastAPI()
router: APIRouter = APIRouter(prefix="/api")


@app.get("/health")
def health():
    return {"ok": True}


@router.post("/predict")
async def predict(_request: Request):
    return {"score": 1.0}
