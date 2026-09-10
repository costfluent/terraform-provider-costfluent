#!/usr/bin/env bash
# Validate every documented example against the provider's own schema.
#
# The examples are what the Terraform Registry shows on each resource and data-source page, so a
# stale one is a published instruction that does not work. They drift silently: nothing in a Go
# build or test reads them, and `make docs` copies whatever it finds into docs/ without judgement.
# Every example here once used block syntax for attributes the schema had already moved to lists.
set -euo pipefail

PROVIDER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v terraform >/dev/null || { echo 'terraform is required to validate the examples' >&2; exit 1; }

# Every go command here reads this module, whatever directory the caller ran the script from.
cd "$PROVIDER_ROOT"

readonly VERSION=0.1.0
readonly NAMESPACE=costfluent

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mirror="$work/plugins/registry.terraform.io/$NAMESPACE/costfluent/$VERSION/$(go env GOOS)_$(go env GOARCH)"
mkdir -p "$mirror"
go build -o "$mirror/terraform-provider-costfluent_v$VERSION" "$PROVIDER_ROOT/cmd/terraform-provider-costfluent"

failed=0
for example in "$PROVIDER_ROOT"/examples/*/*/*.tf "$PROVIDER_ROOT"/examples/provider/provider.tf; do
  [[ -f "$example" ]] || continue
  name="$(basename "$(dirname "$example")")"
  dir="$work/$name"
  mkdir -p "$dir"
  cp "$example" "$dir/"

  # The provider example declares its own terraform block; every other one needs one, plus a key,
  # because a provider that cannot configure itself fails validation before reaching the example.
  if [[ "$name" != provider || "$example" != *examples/provider/* ]]; then
    cat > "$dir/zz-harness.tf" <<TF
terraform {
  required_providers {
    costfluent = {
      source  = "$NAMESPACE/costfluent"
      version = "$VERSION"
    }
  }
}

provider "costfluent" {
  api_key = "validation-only"
}
TF
  fi

  terraform -chdir="$dir" init -plugin-dir="$work/plugins" -input=false >/dev/null
  if ! output="$(terraform -chdir="$dir" validate -no-color 2>&1)"; then
    failed=1
    echo "── ${example#"$PROVIDER_ROOT"/}"
    echo "$output" | sed 's/^/    /'
  fi
done

(( failed == 0 )) || { echo "examples do not match the provider schema; fix them and run make docs" >&2; exit 1; }
echo "ok: every example validates against the provider schema"
