# Ubuntu 24.04/glibc CI tooling for static-contract-gates. The caller supplies
# an immutable base ref, initially Ubuntu upstream and then its GHCR mirror.
ARG BASE_IMAGE=ubuntu@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55
FROM ${BASE_IMAGE}

ARG TARGETARCH
RUN case "${TARGETARCH}" in amd64|arm64) ;; *) exit 1 ;; esac

ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin \
    CGO_ENABLED=1

# Direct apt dependencies carry Ubuntu 24.04 package version pins. The build
# records all transitive versions in the image inventory emitted below.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates=20260601~24.04.1 \
      curl=8.5.0-2ubuntu10.15 \
      git=1:2.43.0-1ubuntu7.3 \
      jq=1.7.1-3ubuntu0.24.04.2 \
      python3=3.12.3-0ubuntu2.1 \
      build-essential=12.10ubuntu1 \
      pkg-config=1.8.1-2build1 \
      gh=2.45.0-1ubuntu0.3 \
      perl=5.38.2-3.2ubuntu0.6 \
    && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.26.9
ARG RIPGREP_VERSION=14.1.1
ARG YQ_VERSION=4.54.1
ARG DOCKER_VERSION=29.1.3
ARG COMPOSE_VERSION=2.40.3

RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) go_sha=42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d; \
             rg_target=x86_64-unknown-linux-musl; \
             rg_sha=4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e; \
             yq_sha=8e34fc298390875de416e6a4afcb8cabeceb25d9aa8506c1a2f9353cf702ea5f; \
             docker_arch=x86_64; \
             docker_sha=c019c608ba2bb009dd673f3230e4d743f36a78d36166c6c2444c05d0aa9ff0d9; \
             compose_sha=dba9d98e1ba5bfe11d88c99b9bd32fc4a0624a30fafe68eea34d61a3e42fd372 ;; \
      arm64) go_sha=4a97373d49fcacdcf3694fea368a500b00ee3e963974f3e7514132717632f052; \
             rg_target=aarch64-unknown-linux-gnu; \
             rg_sha=c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f; \
             yq_sha=189088da0c6429ec5178dfaab1a114805f6cab0b61b165ab236efedf1d57a71b; \
             docker_arch=aarch64; \
             docker_sha=2219f3ac48727d8c5546b13d5d271a58856188ecb8ff1f71f9e2f9aefde3ea76; \
             compose_sha=d26373b19e89160546d15407516cc59f453030d9bc5b43ba7faf16f7b4980137 ;; \
    esac; \
    curl -fsSL --retry 3 -o /tmp/go.tar.gz "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    echo "${go_sha}  /tmp/go.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    curl -fsSL --retry 3 -o /tmp/rg.tar.gz "https://github.com/BurntSushi/ripgrep/releases/download/${RIPGREP_VERSION}/ripgrep-${RIPGREP_VERSION}-${rg_target}.tar.gz"; \
    echo "${rg_sha}  /tmp/rg.tar.gz" | sha256sum -c -; \
    mkdir -p /tmp/rg; \
    tar -C /tmp/rg -xzf /tmp/rg.tar.gz; \
    install -m 0755 "/tmp/rg/ripgrep-${RIPGREP_VERSION}-${rg_target}/rg" /usr/local/bin/rg; \
    curl -fsSL --retry 3 -o /tmp/yq "https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${TARGETARCH}"; \
    echo "${yq_sha}  /tmp/yq" | sha256sum -c -; \
    install -m 0755 /tmp/yq /usr/local/bin/yq; \
    curl -fsSL --retry 3 -o /tmp/docker.tgz "https://download.docker.com/linux/static/stable/${docker_arch}/docker-${DOCKER_VERSION}.tgz"; \
    echo "${docker_sha}  /tmp/docker.tgz" | sha256sum -c -; \
    mkdir -p /tmp/docker; \
    tar -C /tmp/docker -xzf /tmp/docker.tgz docker/docker; \
    install -m 0755 /tmp/docker/docker/docker /usr/local/bin/docker; \
    curl -fsSL --retry 3 -o /tmp/compose "https://github.com/docker/compose/releases/download/v${COMPOSE_VERSION}/docker-compose-linux-${docker_arch}"; \
    echo "${compose_sha}  /tmp/compose" | sha256sum -c -; \
    mkdir -p /usr/local/libexec/docker/cli-plugins; \
    install -m 0755 /tmp/compose /usr/local/libexec/docker/cli-plugins/docker-compose; \
    mkdir -p /usr/local/share/licenses/ci-tools; \
    curl -fsSL --retry 3 -o /tmp/license "https://raw.githubusercontent.com/BurntSushi/ripgrep/${RIPGREP_VERSION}/LICENSE-MIT"; \
    echo '0f96a83840e146e43c0ec96a22ec1f392e0680e6c1226e6f3ba87e0740af850f  /tmp/license' | sha256sum -c -; \
    install -m 0644 /tmp/license /usr/local/share/licenses/ci-tools/ripgrep.LICENSE; \
    curl -fsSL --retry 3 -o /tmp/license "https://raw.githubusercontent.com/mikefarah/yq/v${YQ_VERSION}/LICENSE"; \
    echo '697db34dabb21562fe84487a2ccd031fbd45382b89c2cbdec8ef31682c486040  /tmp/license' | sha256sum -c -; \
    install -m 0644 /tmp/license /usr/local/share/licenses/ci-tools/yq.LICENSE; \
    curl -fsSL --retry 3 -o /tmp/license "https://raw.githubusercontent.com/docker/cli/v${DOCKER_VERSION}/LICENSE"; \
    echo '2d81ea060825006fc8f3fe28aa5dc0ffeb80faf325b612c955229157b8c10dc0  /tmp/license' | sha256sum -c -; \
    install -m 0644 /tmp/license /usr/local/share/licenses/ci-tools/docker-cli.LICENSE; \
    curl -fsSL --retry 3 -o /tmp/license "https://raw.githubusercontent.com/docker/compose/v${COMPOSE_VERSION}/LICENSE"; \
    echo '58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd  /tmp/license' | sha256sum -c -; \
    install -m 0644 /tmp/license /usr/local/share/licenses/ci-tools/docker-compose.LICENSE; \
    rm -rf /tmp/go.tar.gz /tmp/rg.tar.gz /tmp/rg /tmp/yq /tmp/docker.tgz /tmp/docker /tmp/compose /tmp/license; \
    mkdir -p /usr/local/share/ci-tools; \
    dpkg-query -W -f='${Package}\t${Version}\n' > /usr/local/share/ci-tools/packages.tsv; \
    printf 'go\t%s\t%s\nyq\t%s\t%s\nripgrep\t%s\t%s\ndocker-cli\t%s\t%s\ndocker-compose\t%s\t%s\n' \
      "${GO_VERSION}" "${go_sha}" "${YQ_VERSION}" "${yq_sha}" \
      "${RIPGREP_VERSION}" "${rg_sha}" "${DOCKER_VERSION}" "${docker_sha}" \
      "${COMPOSE_VERSION}" "${compose_sha}" > /usr/local/share/ci-tools/downloads.tsv

# The digest plus package inventory is captured by the publishing workflow.
# The image contains tooling only; source is mounted by Actions at execution.
WORKDIR /workspace
