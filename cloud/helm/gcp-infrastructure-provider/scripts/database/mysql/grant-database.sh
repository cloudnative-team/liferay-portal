#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
    local scripts_dir

    scripts_dir=$(dirname "${0}")

    sh "${scripts_dir}/check-database-login.sh"

    mysql \
        --host 127.0.0.1 \
        --port "${DATABASE_PORT}" \
        --user "${DATABASE_USERNAME}" <<EOF
SET ROLE ALL;

GRANT ALL PRIVILEGES ON ${DATABASE_NAME}.* TO '${LIFERAY_DATABASE_USERNAME}'@'%';
EOF
}

main