FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /connect-slicer-bridge .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /connect-slicer-bridge /connect-slicer-bridge
EXPOSE 17481
ENTRYPOINT ["/connect-slicer-bridge"]
