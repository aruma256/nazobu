#!/bin/sh
# nazobu を NAS 上で再デプロイする。
# Synology host には git が入っていないため、git pull は alpine/git コンテナで実行する。
set -e
cd /volume1/docker/nazobu

# main を fetch & merge
sudo docker run --rm \
  -v /volume1/docker/nazobu:/repo \
  -w /repo \
  mirror.gcr.io/alpine/git \
  -c safe.directory=/repo pull origin main

# 停止しているサービスがあれば起動 / compose 設定が変わっていれば反映（初回・cloudflared 等向け）
sudo docker compose -f compose.yaml -f compose.prod.yaml up -d

# backend / frontend は bind mount のソースが更新されているので強制再作成して再ビルド
sudo docker compose -f compose.yaml -f compose.prod.yaml up -d --force-recreate --no-deps backend frontend
