# Cheap GKE substrate

This OpenTofu module targets an existing billing-enabled project supplied through the required `project_id` variable. It creates a zonal GKE Standard cluster with one Spot `e2-standard-2` node, a VPC-native subnet, Artifact Registry, an MLflow GCS bucket, and separate Workload Identity service accounts for agentgateway and MLflow. It creates no Cloud NAT, Ingress, or public LoadBalancer.

Before planning, run `mise run install:gcp`, authenticate Application Default Credentials, run `GCP_PROJECT_ID=<project-id> mise run doctor:gcp` from the repository root, and set the same project plus your public `/32` in a gitignored `terraform.tfvars` based on `terraform.tfvars.example`.

```bash
tofu init
tofu validate
tofu plan -out=tfplan
```

Review the plan and current GCP prices before a later, explicitly approved `tofu apply tfplan`. After apply, `tofu output -raw get_credentials_command` prints the command that configures kubectl. `../scripts/render-gke.sh` resolves the Workload Identity service accounts, MLflow bucket, and Vertex project from OpenTofu outputs; the committed manifests contain fail-visible placeholders instead of a project ID.

Spot VMs can stop at any time. The GKE overlay uses zonal standard persistent disks for the small PersistentVolumeClaims, while the GCS bucket preserves MLflow artifacts. The fixed estimate checked on 29 July 2026 is about USD 28/month with the GKE credit or USD 101 without it. Refresh Spot and usage prices before applying.
