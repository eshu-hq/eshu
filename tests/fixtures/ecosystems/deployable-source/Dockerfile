# Builds the image ghcr.io/acme/deployable-source, deployed by the
# deployable-config repo (rc-1: CORRELATES_DEPLOYABLE_UNIT). IP-free fixture.
FROM gcr.io/distroless/base-debian12
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
