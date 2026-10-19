# B-7 sibling-seed fixture (#8540). This Dockerfile names the
# deployable-config deployable unit for the Jenkins repository, so the
# workload materializer pairs it SAME_NAME with the deployable-config
# repository's own workload, giving the catalog SAME_NAME sibling read its
# corpus coverage. The pair name deliberately avoids deployable-source:
# that name carries legacy workload:deployable-source cloud anchors that
# the #5385 handle ladder fails closed when ambiguous (see #8574). Base
# image mirrors the deployable-source fixture's own Dockerfile so no new
# image identity surface enters the corpus. IP-free fixture.
FROM gcr.io/distroless/base-debian12
ENTRYPOINT ["/app"]
