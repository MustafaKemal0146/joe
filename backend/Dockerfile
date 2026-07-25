FROM python:3.12-slim AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       curl \
       tesseract-ocr \
       tesseract-ocr-eng \
       tesseract-ocr-tur \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /srv/joe
COPY backend/pyproject.toml ./pyproject.toml
RUN python -c "import tomllib; data=tomllib.load(open('pyproject.toml','rb')); print('\\n'.join(data['project']['dependencies']))" > /tmp/joe-requirements.txt \
    && pip install --no-cache-dir -r /tmp/joe-requirements.txt \
    && rm /tmp/joe-requirements.txt
COPY backend/app ./app
COPY backend/alembic.ini ./alembic.ini
COPY backend/alembic ./alembic

RUN useradd --create-home --uid 10001 joe \
    && mkdir -p /data/artifacts /data/secrets \
    && chown -R joe:joe /data /srv/joe

USER joe
ENV JOE_DATA_DIR=/data

EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
