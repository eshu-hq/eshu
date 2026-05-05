FROM python:3.12-slim

LABEL org.opencontainers.image.source="example/service-gha"

WORKDIR /app
COPY app.py /app/app.py

EXPOSE 8080

CMD ["python", "/app/app.py"]
