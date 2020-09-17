# We use the base overseer image, so that we can run a cronjob that will enqueue our tests as desired
FROM cmaster11/overseer:1.13.3
# Inspired by https://github.com/nehabhardwaj01/docker-cron
# Inspired by https://forums.docker.com/t/how-to-run-a-cron-job-inside-a-container-alpine/7759/7

RUN apk add bash

# Add files
ADD entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT bash /entrypoint.sh