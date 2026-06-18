# Synthetic supply-chain demo image (issue #3019).
#
# This image packages the synthetic vulnerable demo app in app/. It exists so
# the demo can produce a real container image identity (a sha256 digest) for the
# image -> SBOM-subject -> workload half of the chain. Everything in it is
# synthetic: the app makes no real network calls and depends only on a synthetic
# vulnerable package declared in app/package.json.
#
# Determinism note: pin the base image by digest before recording the demo so
# the produced image digest is reproducible. The tag below is a placeholder;
# replace <PIN_BY_DIGEST> with the digest you build against, for example:
#   FROM node:22-alpine@sha256:<digest>
# Pinning by digest is what makes the produced image digest stable across
# machines, which the SBOM subject in sbom/app.cdx.json must match.
FROM node:22-alpine
# Replace the line above with the digest-pinned form before recording:
#   FROM node:22-alpine@sha256:<PIN_BY_DIGEST>

WORKDIR /app

# Copy the manifest and lockfile first for cacheable, reproducible installs.
COPY app/package.json app/package-lock.json ./

# The synthetic dependency does not exist on a real registry, so `npm ci` is not
# run here; the lockfile is what Eshu's repository scan reads. A real demo app
# would run `npm ci --omit=dev` against a registry that hosts the package.

COPY app/server.js ./

ENV PORT=8080
EXPOSE 8080

# Run as a non-root user for a minimally responsible demo image.
USER node

CMD ["node", "server.js"]
