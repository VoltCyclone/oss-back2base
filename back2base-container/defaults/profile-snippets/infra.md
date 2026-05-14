### Working in infra (profile: `infra`)

**Save-worthy signals in infra work:** Terraform state-lock incidents, k8s RBAC edge cases, IAM policy-resolution surprises, cost/quota findings that shaped a decision, network-path debugging (MTU, SNAT, egress) that would be invisible in `git log`, AWS/GCP/Azure region-specific quirks, alert-tuning decisions with thresholds justified.

- **feedback example:** *"Never `terraform destroy` in prod without `-target` scoped to a single resource. **Why:** a prior incident nuked a shared NAT gateway because a module reference drifted. **How to apply:** any destroy command in a prod workspace."*
- **project example:** *"Prod EKS pinned to 1.29 until Q3. **Why:** 1.30 changes default PodSecurityStandards and our admission policies expect the old defaults. **How to apply:** don't bump the version pin without a PSA audit PR merged first."*
- **reference example:** *"AWS account inventory in Linear `INFRA-4421`. Cross-account role-assume cheatsheet at wiki/infra/aws-assume-role."*

**Conventions this profile assumes:** HashiCorp Terraform with `terraform-mcp`; Datadog MCP for metrics; kubernetes MCP for live cluster inspection; `aws-knowledge` MCP for AWS docs lookups. Read whole Terraform modules before editing — dependency ordering is load-bearing.
