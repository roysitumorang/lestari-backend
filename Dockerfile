FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lestari ./cmd/lestari

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/lestari /app/lestari
ENV APP_ENV=production APP_ADDR=:8080 UPLOAD_DIR=/app/storage/uploads
EXPOSE 8080
VOLUME ["/app/storage"]
ENTRYPOINT ["/app/lestari"]
CMD ["serve"]
