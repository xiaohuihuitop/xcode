#!/usr/bin/env bash

set -Eeuo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
PRUNER="${DEPLOY_DIR}/backup-retention/xhh-prune-backups"
TEST_ROOT=""
NOW_EPOCH=1787587200

cleanup() {
    if [[ -n "${TEST_ROOT}" && -d "${TEST_ROOT}" ]]; then
        rm -rf -- "${TEST_ROOT}"
    fi
}
trap cleanup EXIT

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_exists() {
    [[ -e "$1" || -L "$1" ]] || fail "Expected path to exist: $1"
}

assert_missing() {
    [[ ! -e "$1" && ! -L "$1" ]] || fail "Expected path to be absent: $1"
}

assert_contains() {
    local needle="$1"
    local file="$2"
    grep -Fq -- "${needle}" "${file}" || fail "Expected ${file} to contain: ${needle}"
}

reset_fixture() {
    cleanup
    TEST_ROOT="$(mktemp -d -t xhh-prune-backups-test.XXXXXX)"
    SERVER_ROOT="${TEST_ROOT}/opt/server-backups"
    UPGRADE_ROOT="${TEST_ROOT}/opt/xcode/backups"
    OUTPUT_FILE="${TEST_ROOT}/output.log"
    mkdir -p "${SERVER_ROOT}" "${UPGRADE_ROOT}"
}

run_pruner() {
    XHH_PRUNE_ALLOW_TEST_ROOTS=1 \
        "${PRUNER}" --test-root-prefix "${TEST_ROOT}" --now-epoch "${NOW_EPOCH}" "$@"
}

set_mtime() {
    touch -d "@$2" "$1"
}

test_daily_boundary_and_dry_run() {
    reset_fixture

    local recent="${SERVER_ROOT}/server-backup-recent.tar.gz"
    local boundary="${SERVER_ROOT}/server-backup-boundary.tar.gz"
    local old="${SERVER_ROOT}/server-backup-old.tar.gz"
    local partial="${SERVER_ROOT}/server-backup-incomplete.tar.gz.partial"
    local staging="${SERVER_ROOT}/.staging-current"
    local unrelated="${SERVER_ROOT}/manual-backup.tar.gz"
    local symlink="${SERVER_ROOT}/server-backup-link.tar.gz"

    touch "${recent}" "${boundary}" "${old}" "${partial}" "${staging}" "${unrelated}"
    ln -s "${old}" "${symlink}"
    set_mtime "${recent}" "$((NOW_EPOCH - 4319 * 60))"
    set_mtime "${boundary}" "$((NOW_EPOCH - 4320 * 60))"
    set_mtime "${old}" "$((NOW_EPOCH - 4321 * 60))"
    printf 'original-index\n' >"${SERVER_ROOT}/backup-index.txt"

    run_pruner >"${OUTPUT_FILE}"
    assert_exists "${old}"
    assert_contains "original-index" "${SERVER_ROOT}/backup-index.txt"
    assert_contains "action=would_delete type=daily" "${OUTPUT_FILE}"
    assert_contains "reason=older_than_72_hours" "${OUTPUT_FILE}"
    assert_contains "action=skip type=daily reason=symlink" "${OUTPUT_FILE}"

    run_pruner --apply >"${OUTPUT_FILE}"
    assert_exists "${recent}"
    assert_exists "${boundary}"
    assert_missing "${old}"
    assert_exists "${partial}"
    assert_exists "${staging}"
    assert_exists "${unrelated}"
    assert_exists "${symlink}"
    assert_contains "server-backup-boundary.tar.gz" "${SERVER_ROOT}/backup-index.txt"
    assert_contains "server-backup-recent.tar.gz" "${SERVER_ROOT}/backup-index.txt"
    mapfile -t index_lines <"${SERVER_ROOT}/backup-index.txt"
    [[ "${index_lines[0]}" == "server-backup-boundary.tar.gz" ]] || \
        fail "backup-index.txt did not preserve ascending filename order"
    [[ "${index_lines[1]}" == "server-backup-recent.tar.gz" ]] || \
        fail "backup-index.txt did not preserve ascending filename order"
    if grep -Fq 'server-backup-old.tar.gz' "${SERVER_ROOT}/backup-index.txt"; then
        fail "Deleted daily backup remained in backup-index.txt"
    fi
}

create_upgrade() {
    local name="$1"
    local mtime="$2"
    mkdir -p "${UPGRADE_ROOT}/${name}"
    set_mtime "${UPGRADE_ROOT}/${name}" "${mtime}"
}

test_upgrade_versions_and_duplicate_retention() {
    reset_fixture

    create_upgrade "v1.0.8-before-v1.0.9-20260824010000" "$((NOW_EPOCH - 500))"
    create_upgrade "v1.0.9-before-v1.0.10-20260824020000" "$((NOW_EPOCH - 400))"
    create_upgrade "v1.0.10-before-v1.0.11-20260824030000" "$((NOW_EPOCH - 300))"
    create_upgrade "v1.0.11-before-v1.0.12-20260824040000" "$((NOW_EPOCH - 200))"
    create_upgrade "v1.0.11-before-v1.0.12-20260824050000" "$((NOW_EPOCH - 100))"

    run_pruner >"${OUTPUT_FILE}"
    assert_contains "reason=duplicate_target_version" "${OUTPUT_FILE}"
    assert_contains "reason=target_version_outside_latest_three" "${OUTPUT_FILE}"
    assert_exists "${UPGRADE_ROOT}/v1.0.8-before-v1.0.9-20260824010000"

    run_pruner --apply >"${OUTPUT_FILE}"
    assert_missing "${UPGRADE_ROOT}/v1.0.8-before-v1.0.9-20260824010000"
    assert_exists "${UPGRADE_ROOT}/v1.0.9-before-v1.0.10-20260824020000"
    assert_exists "${UPGRADE_ROOT}/v1.0.10-before-v1.0.11-20260824030000"
    assert_missing "${UPGRADE_ROOT}/v1.0.11-before-v1.0.12-20260824040000"
    assert_exists "${UPGRADE_ROOT}/v1.0.11-before-v1.0.12-20260824050000"
}

test_semver_compares_each_numeric_component() {
    reset_fixture

    create_upgrade "v1.0.98-before-v1.0.99-20260824010000" "$((NOW_EPOCH - 400))"
    create_upgrade "v1.0.99-before-v1.1.0-20260824020000" "$((NOW_EPOCH - 300))"
    create_upgrade "v1.1.0-before-v1.1.1-20260824030000" "$((NOW_EPOCH - 200))"
    create_upgrade "v1.9.9-before-v2.0.0-20260824040000" "$((NOW_EPOCH - 100))"

    run_pruner --apply >"${OUTPUT_FILE}"
    assert_missing "${UPGRADE_ROOT}/v1.0.98-before-v1.0.99-20260824010000"
    assert_exists "${UPGRADE_ROOT}/v1.0.99-before-v1.1.0-20260824020000"
    assert_exists "${UPGRADE_ROOT}/v1.1.0-before-v1.1.1-20260824030000"
    assert_exists "${UPGRADE_ROOT}/v1.9.9-before-v2.0.0-20260824040000"
}

test_invalid_targets_and_symlinks_are_skipped() {
    reset_fixture

    local invalid="${UPGRADE_ROOT}/v1.0.11-before-vnot-semver-20260825010000"
    local malformed="${UPGRADE_ROOT}/unexpected-backup"
    local wrong_type="${UPGRADE_ROOT}/v1.0.11-before-v1.0.12-20260825020000"
    local target="${TEST_ROOT}/outside-upgrade"
    local symlink="${UPGRADE_ROOT}/v1.0.11-before-v1.0.13-20260825030000"

    mkdir -p "${invalid}" "${malformed}" "${target}"
    touch "${wrong_type}"
    ln -s "${target}" "${symlink}"

    run_pruner --apply >"${OUTPUT_FILE}"
    assert_exists "${invalid}"
    assert_exists "${malformed}"
    assert_exists "${wrong_type}"
    assert_exists "${symlink}"
    assert_contains "reason=invalid_target_version" "${OUTPUT_FILE}"
    assert_contains "action=skip type=upgrade reason=wrong_type" "${OUTPUT_FILE}"
    assert_contains "action=skip type=upgrade reason=symlink" "${OUTPUT_FILE}"
}

test_rejects_unsafe_test_root() {
    reset_fixture

    local sentinel="${SERVER_ROOT}/server-backup-sentinel.tar.gz"
    touch "${sentinel}"
    if XHH_PRUNE_ALLOW_TEST_ROOTS=1 \
        "${PRUNER}" --test-root-prefix "/tmp/not-an-xhh-test-root" --apply >"${OUTPUT_FILE}" 2>&1; then
        fail "Pruner accepted an unsafe test root"
    fi
    assert_exists "${sentinel}"
}

[[ -x "${PRUNER}" ]] || fail "Pruner is missing or not executable: ${PRUNER}"

test_daily_boundary_and_dry_run
test_upgrade_versions_and_duplicate_retention
test_semver_compares_each_numeric_component
test_invalid_targets_and_symlinks_are_skipped
test_rejects_unsafe_test_root

printf 'Backup retention tests passed.\n'
