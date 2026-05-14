FROM --platform=linux/amd64 debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata wget && rm -rf /var/lib/apt/lists/*

COPY new-api /app/new-api
RUN chmod +x /app/new-api

EXPOSE 3000

WORKDIR /data

ENTRYPOINT ["/app/new-api"]
