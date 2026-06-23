# -------------------------------------
# ------------BUILD STAGE--------------
# -------------------------------------

FROM golang:1.26.4-alpine AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /goore ./cmd/server/

# -------------------------------------
# -----------RUNTIME STAGE-------------
# -------------------------------------

FROM gcr.io/distroless/base-debian13 AS release

WORKDIR /

COPY --from=build-stage /goore /goore

USER nonroot:nonroot

ENTRYPOINT ["/goore"]