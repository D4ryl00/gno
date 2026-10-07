# gnokey-pair's relay: the reference magic-wormhole mailbox server, unmodified.
#
#   docker build -t gnokey-pair-relay contribs/gnokey-pair/relay
#   docker run -d -p 4000:4000 gnokey-pair-relay
#
# It serves ws://<host>:4000/v1; put a TLS proxy in front for wss://.
# /data holds only pairings in progress; mount a volume there to keep them
# when the container is replaced.

FROM python:3.12-slim

COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --root-user-action=ignore --requirement /tmp/requirements.txt \
    && rm /tmp/requirements.txt \
    && useradd --system --no-create-home relay \
    && mkdir /data && chown relay /data

USER relay
WORKDIR /data
EXPOSE 4000

# No --usage-db: the relay keeps no usage records.
CMD ["twist", "--log-format=text", "wormhole-mailbox", "--port", "tcp:4000", "--channel-db", "/data/relay.sqlite"]
