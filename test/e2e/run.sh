#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

TEST_DIR="${ROOT_DIR}/test/e2e"
SCENARIOS_DIR="${TEST_DIR}/scenarios"
COMPOSE_FILE="${TEST_DIR}/docker-compose.yml"
BIN_DIR="${TEST_DIR}/.bin"
TMP_DIR="${TEST_DIR}/.tmp"
TERRAFORM_IMAGE="${TERRAFORM_IMAGE:-hashicorp/terraform:1.9.8}"
GO_ARCH="${GO_ARCH:-$(go env GOARCH)}"
PROVIDER_BINARY="${BIN_DIR}/terraform-provider-cassandra"

log() {
  printf '\n[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

cleanup() {
  log "Stopping Docker services"
  docker compose -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
}

prepare_dirs() {
  mkdir -p "${BIN_DIR}" "${TMP_DIR}"
}

write_terraform_rc() {
  cat >"${TMP_DIR}/terraformrc" <<EOF
provider_installation {
  dev_overrides {
    "kyawmyintthein/cassandra" = "/workspace/test/e2e/.bin"
  }
  direct {}
}
EOF
}

build_provider() {
  log "Building Linux provider binary for Docker-based Terraform"
  GOCACHE="${ROOT_DIR}/.gocache" \
    GOMODCACHE="${ROOT_DIR}/.gomodcache" \
    GOOS=linux GOARCH="${GO_ARCH}" CGO_ENABLED=0 \
    go build -o "${PROVIDER_BINARY}" .
}

start_cassandra() {
  log "Starting Cassandra container"
  docker compose -f "${COMPOSE_FILE}" up -d --wait cassandra
}

terraform_in_docker() {
  local workdir="$1"
  shift

  docker run --rm \
    --network cassandra-e2e \
    --user "$(id -u):$(id -g)" \
    -v "${ROOT_DIR}:/workspace" \
    -w "/workspace/${workdir}" \
    -e TF_CLI_CONFIG_FILE=/workspace/test/e2e/.tmp/terraformrc \
    "${TERRAFORM_IMAGE}" "$@"
}

copy_phase_into_workdir() {
  local phase_dir="$1"
  local workdir="$2"

  find "${workdir}" -maxdepth 1 -type f \( -name '*.tf' -o -name '*.tf.json' -o -name '*.tfvars' -o -name '.terraform.lock.hcl' \) -delete
  find "${phase_dir}" -mindepth 1 -maxdepth 1 -type f \( -name '*.tf' -o -name '*.tfvars' -o -name '.terraform.lock.hcl' \) -exec cp {} "${workdir}/" \;
}

run_assertions() {
  local phase_dir="$1"
  if [[ -f "${phase_dir}/assert.sh" ]]; then
    ROOT_DIR="${ROOT_DIR}" COMPOSE_FILE="${COMPOSE_FILE}" bash "${phase_dir}/assert.sh"
  fi
}

phase_state_name() {
  local phase_dir="$1"

  if [[ -f "${phase_dir}/state_name" ]]; then
    tr -d '[:space:]' <"${phase_dir}/state_name"
    return
  fi

  printf 'default'
}

run_phase() {
  local scenario_name="$1"
  local phase_dir="$2"
  local workdir_relative="$3"
  local workdir_absolute="${ROOT_DIR}/${workdir_relative}"
  local phase_name
  phase_name="$(basename "${phase_dir}")"

  log "Scenario ${scenario_name}: ${phase_name}"
  copy_phase_into_workdir "${phase_dir}" "${workdir_absolute}"

  if [[ -f "${phase_dir}/expected_error.txt" ]]; then
    local expected
    expected="$(<"${phase_dir}/expected_error.txt")"
    local output
    set +e
    output="$(terraform_in_docker "${workdir_relative}" apply -auto-approve -no-color 2>&1)"
    local status=$?
    set -e
    if [[ ${status} -eq 0 ]]; then
      echo "${output}" >&2
      echo "Expected phase ${scenario_name}/${phase_name} to fail, but it succeeded." >&2
      exit 1
    fi
    if [[ "${output}" != *"${expected}"* ]]; then
      echo "${output}" >&2
      echo "Expected failure output to include: ${expected}" >&2
      exit 1
    fi
    return 10
  fi

  if ! terraform_in_docker "${workdir_relative}" apply -auto-approve -no-color >/dev/null; then
    return 1
  fi
  if ! run_assertions "${phase_dir}"; then
    return 1
  fi
  return 0
}

destroy_scenario() {
  local workdir_relative="$1"

  if [[ -f "${ROOT_DIR}/${workdir_relative}/main.tf" ]]; then
    terraform_in_docker "${workdir_relative}" destroy -auto-approve -no-color >/dev/null || true
  fi
}

run_scenario() {
  local scenario_dir="$1"
  local scenario_name
  scenario_name="$(basename "${scenario_dir}")"
  local scenario_tmp_root="${ROOT_DIR}/test/e2e/.tmp/workdir-${scenario_name}"
  local scenario_meta_root="${ROOT_DIR}/test/e2e/.tmp/meta-${scenario_name}"

  rm -rf "${scenario_tmp_root}" "${scenario_meta_root}"
  mkdir -p "${scenario_tmp_root}" "${scenario_meta_root}"

  while IFS= read -r phase_dir; do
    local state_name
    state_name="$(phase_state_name "${phase_dir}")"
    local workdir_relative="test/e2e/.tmp/workdir-${scenario_name}/${state_name}"
    local workdir_absolute="${ROOT_DIR}/${workdir_relative}"

    mkdir -p "${workdir_absolute}"

    if run_phase "${scenario_name}" "${phase_dir}" "${workdir_relative}"; then
      printf '%s' "${phase_dir}" >"${scenario_meta_root}/${state_name}"
      continue
    else
      local phase_status=$?
      if [[ ${phase_status} -eq 10 ]]; then
        continue
      fi

      return "${phase_status}"
    fi
  done < <(find "${scenario_dir}" -mindepth 1 -maxdepth 1 -type d | sort)

  while IFS= read -r state_file; do
    local state_name
    state_name="$(basename "${state_file}")"
    local last_successful_phase
    last_successful_phase="$(<"${state_file}")"
    local workdir_relative="test/e2e/.tmp/workdir-${scenario_name}/${state_name}"
    local workdir_absolute="${ROOT_DIR}/${workdir_relative}"

    copy_phase_into_workdir "${last_successful_phase}" "${workdir_absolute}"
    destroy_scenario "${workdir_relative}"
  done < <(find "${scenario_meta_root}" -mindepth 1 -maxdepth 1 -type f | sort)

  log "Scenario ${scenario_name} completed"
}

main() {
  trap cleanup EXIT
  prepare_dirs
  write_terraform_rc
  build_provider
  start_cassandra

  local scenarios=()
  if [[ $# -gt 0 ]]; then
    for name in "$@"; do
      scenarios+=("${SCENARIOS_DIR}/${name}")
    done
  else
    while IFS= read -r scenario_dir; do
      scenarios+=("${scenario_dir}")
    done < <(find "${SCENARIOS_DIR}" -mindepth 1 -maxdepth 1 -type d | sort)
  fi

  for scenario in "${scenarios[@]}"; do
    if [[ ! -d "${scenario}" ]]; then
      echo "Unknown scenario: ${scenario}" >&2
      exit 1
    fi
    run_scenario "${scenario}"
  done

  log "All end-to-end scenarios passed"
}

main "$@"
