#!/usr/bin/env bash
# Regenerate docs/ from the provider schema and the examples in examples/.
#
# tfplugindocs would normally build the provider itself, but it builds the package at the module
# root and this provider's main lives under cmd/. So the schema is produced here — a local build,
# a throwaway Terraform working directory pointed at it, and `terraform providers schema` — and
# handed to tfplugindocs, which then skips its own build entirely.
set -euo pipefail

PROVIDER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TFPLUGINDOCS_VERSION="${TFPLUGINDOCS_VERSION:?set by the GNUmakefile}"

# Both are arbitrary and neither reaches the generated pages: nothing is released from here, and
# the schema does not depend on what the throwaway working directory calls the provider it loads.
# The namespace is `hashicorp` because that is the address tfplugindocs looks for in the schema
# JSON — under any other namespace it reports the provider as missing from a file that contains it.
readonly VERSION=0.1.0
readonly NAMESPACE=hashicorp

command -v terraform >/dev/null || { echo 'terraform is required to export the provider schema' >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mirror="$work/plugins/registry.terraform.io/$NAMESPACE/costfluent/$VERSION/$(go env GOOS)_$(go env GOARCH)"
mkdir -p "$mirror"
go build -o "$mirror/terraform-provider-costfluent_v$VERSION" "$PROVIDER_ROOT/cmd/terraform-provider-costfluent"

cat > "$work/main.tf" <<TF
terraform {
  required_providers {
    costfluent = {
      source  = "$NAMESPACE/costfluent"
      version = "$VERSION"
    }
  }
}
TF

terraform -chdir="$work" init -plugin-dir="$work/plugins" -input=false >/dev/null
terraform -chdir="$work" providers schema -json > "$work/schema.json"

go run "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$TFPLUGINDOCS_VERSION" generate \
  --provider-dir "$PROVIDER_ROOT" \
  --provider-name costfluent \
  --rendered-provider-name Costfluent \
  --providers-schema "$work/schema.json"
