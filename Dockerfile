FROM gcr.io/distroless/static:nonroot
ARG TARGETPLATFORM
WORKDIR /
COPY ${TARGETPLATFORM}/kubernetes-landscape-orchestrator .
USER 65532:65532

ENTRYPOINT ["/kubernetes-landscape-orchestrator"]