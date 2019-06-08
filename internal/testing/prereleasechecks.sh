#!/usr/bin/env bash
# Copyright 2019 The Go Cloud Development Kit Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script runs expensive checks that we don't normally run on Travis, but
# that should run periodically, before each release.
# For example, tests that can't use record/replay, so must be performed live
# against the service provider.
#
# It should be run from the root directory.

# https://coderwall.com/p/fkfaqq/safer-bash-scripts-with-set-euxo-pipefail
# Change to -euxo if debugging.
set -euo pipefail

function usage() {
  echo
  echo "Usage: prereleasechecks.sh <init | run | cleanup>" 1>&2
  echo "  init: creates any needed resources; rerun until it succeeds"
  echo "  run: runs all needed checks"
  echo "  cleanup: cleans up resources created in init"
  exit 64
}

if [[ $# -ne 1 ]] ; then
  echo "Need at least one argument."
  usage
fi

op="$1"
case "$op" in
  init|run|cleanup);;
  *) echo "Unknown operation '$op'" && usage;;
esac

# TODO: It would be nice to ensure that none of the tests are skipped. For now,
#       we assume that if the "init" steps succeeded, the necessary tests will
#       run.

rootdir="$(pwd)"
FAILURES=""

TESTDIR="mysql/azuremysql"
echo "***** $TESTDIR *****"
pushd "$TESTDIR" &> /dev/null
case "$op" in
  init)
    terraform init && terraform apply -var location="centralus" -var resourcegroup="GoCloud" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
  run)
    # TODO: These tests fail with "Error 9999".
    go test -mod=readonly -race -json | go run "$rootdir"/internal/testing/test-summary/test-summary.go -progress || echo "[KNOWN FAILURE]"
    ;;
  cleanup)
    terraform destroy -var location="centralus" -var resourcegroup="GoCloud" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
esac
popd &> /dev/null


TESTDIR="mysql/cloudmysql"
echo
echo "***** $TESTDIR *****"
pushd "$TESTDIR" &> /dev/null
case "$op" in
  init)
    # TODO: This fails with "Error 403: The caller does not have permission, forbidden".
    terraform init && terraform apply -var project="go-cloud-test-216917" -auto-approve || echo "[KNOWN FAILURE]"
    ;;
  run)
    # TODO: This fails, probably because of the Terraform error above.
    go test -mod=readonly -race -json | go run "$rootdir"/internal/testing/test-summary/test-summary.go -progress || echo "[KNOWN FAILURE]"
    ;;
  cleanup)
    terraform destroy -var project="go-cloud-test-216917" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
esac
popd &> /dev/null


TESTDIR="mysql/rdsmysql"
echo
echo "***** $TESTDIR *****"
pushd "$TESTDIR" &> /dev/null
case "$op" in
  init)
    terraform init && terraform apply -var region="us-west-1" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
  run)
    go test -mod=readonly -race -json | go run "$rootdir"/internal/testing/test-summary/test-summary.go -progress || FAILURES="$FAILURES $TESTDIR"
    ;;
  cleanup)
    terraform destroy -var region="us-west-1" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
esac
popd &> /dev/null


TESTDIR="postgres/cloudpostgres"
echo
echo "***** $TESTDIR *****"
pushd "$TESTDIR" &> /dev/null
case "$op" in
  init)
    # TODO: This fails with "Error 403: The caller does not have permission, forbidden".
    terraform init && terraform apply -var project="go-cloud-test-216917" -auto-approve || echo "[KNOWN FAILURE]"
    ;;
  run)
    # TODO: This fails, probably because of the Terraform error above.
    go test -mod=readonly -race -json | go run "$rootdir"/internal/testing/test-summary/test-summary.go -progress || echo "[KNOWN FAILURE]"
    ;;
  cleanup)
    terraform destroy -var project="go-cloud-test-216917" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
esac
popd &> /dev/null


TESTDIR="postgres/rdspostgres"
echo
echo "***** $TESTDIR *****"
pushd "$TESTDIR" &> /dev/null
case "$op" in
  init)
    terraform init && terraform apply -var region="us-west-1" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
  run)
    go test -mod=readonly -race -json  | go run "$rootdir"/internal/testing/test-summary/test-summary.go -progress || FAILURES="$FAILURES $TESTDIR"
    ;;
  cleanup)
    terraform destroy -var region="us-west-1" -auto-approve || FAILURES="$FAILURES $TESTDIR"
    ;;
esac
popd &> /dev/null


# This iterates over all packages that have a "testdata" directory, using that
# as a signal for record/replay tests, and runs the tests with a "-record" flag.
# This verifies that we can generate a fresh recording against the live
# provider.
while read -r TESTDIR; do
  echo
  echo "***** $TESTDIR *****"
  pushd "$TESTDIR" &> /dev/null
  case "$op" in
    init)
      ;;
    run)
      go test -mod=readonly -race -record -json | go run "$rootdir"/internal/testing/test-summary/test-summary.go -progress || FAILURES="$FAILURES $TESTDIR"
      ;;
    cleanup)
      ;;
  esac
  popd &> /dev/null
done < <( find . -name testdata -printf "%h\n" )

echo
echo
if [ ! -z "$FAILURES" ]; then
  echo "FAILED!"
  echo "Investigate and re-run -record tests for the following packages: $FAILURES"
  exit 1
fi

echo "SUCCESS!"
