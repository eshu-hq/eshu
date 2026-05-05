FROM public.ecr.aws/docker/library/python:3.10-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    UV_CACHE_DIR=/tmp/uv-cache \
    DAGSTER_HOME=/opt/dagster/app

RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir uv

RUN useradd --create-home --shell /bin/bash dagster && \
    mkdir -p $DAGSTER_HOME && \
    chown -R dagster:dagster $DAGSTER_HOME

WORKDIR $DAGSTER_HOME

COPY pyproject.toml uv.lock* ./
COPY setup.cfg ./

RUN uv sync --no-dev && \
    rm -rf /tmp/uv-cache

COPY bg_data_platform/ ./bg_data_platform/

COPY dagster.yaml $DAGSTER_HOME/dagster.yaml

COPY workspace.yaml $DAGSTER_HOME/workspace.yaml

COPY --chmod=755 entrypoint.sh /entrypoint.sh

RUN chown -R dagster:dagster $DAGSTER_HOME

USER dagster

ENV PATH="/opt/dagster/app/.venv/bin:$PATH"

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:3000/server_info || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["dagster", "webserver", "--host", "0.0.0.0", "--port", "3000"] 