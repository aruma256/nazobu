FROM mirror.gcr.io/golang:1.26

WORKDIR /app

# sqldef CLI（mysqldef）を PATH に置く。migrate コマンドからこれを exec する。
ENV GOBIN=/usr/local/bin
RUN go install github.com/sqldef/sqldef/v3/cmd/mysqldef@v3.11.1

# 依存解決のキャッシュを効かせるため go.mod を先に COPY
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

EXPOSE 8080

# 起動時に sqldef で自動マイグレーションしてから start
CMD ["sh", "-c", "go run . migrate && go run . start"]
