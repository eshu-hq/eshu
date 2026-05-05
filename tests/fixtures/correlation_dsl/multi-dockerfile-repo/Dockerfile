FROM python:3.12-slim

LABEL org.opencontainers.image.source="example/multi-dockerfile-repo"

WORKDIR /app
COPY server.py /app/server.py

EXPOSE 8080

CMD ["python", "/app/server.py"]
