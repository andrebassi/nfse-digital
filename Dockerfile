# Imagem do painel para o Fly: binário estático (CGO_ENABLED=0, os drivers
# SQLite e Turso são Go puro) sobre distroless, que traz CA e tzdata — o
# fuso de São Paulo e o TLS mútuo com a SEFIN precisam dos dois.
# O app.css já vem gerado (task css): o Tailwind não entra na imagem.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nfse ./cmd/nfse

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nfse /nfse
EXPOSE 8080
ENTRYPOINT ["/nfse", "serve", "--run-schedules"]
