# Karaboz Cloud

Independent multi-tenant object storage with one versioned REST API for the platform owner and external developers.

## Included foundation
- Go REST API: developer registration/login, projects, scoped API-key creation/revocation, buckets, object upload/download/list/delete, signed download URLs and usage.
- PostgreSQL metadata with project ownership and quotas.
- SeaweedFS through its S3-compatible API.
- React developer console and Docker Compose development stack.

## Run locally
1. Install Docker Compose.
2. Copy .env.example to .env and replace every example secret.
3. Run: docker compose up --build
4. API health: http://localhost:8080/healthz
5. Dashboard: http://localhost:4173

This is an MVP foundation, not a hosted production service. Public launch still requires server deployment, HTTPS, backups, monitoring, secret rotation, email verification, abuse controls, rate limits and operational policies. Initial file uploads are single-request uploads with a size cap; multipart/resumable uploads and billing are later milestones.

## Using the platform
Create your account, create a project for each app/environment, then generate a project API key in the dashboard. The key is displayed once. Other developers register their own accounts and manage only their own projects. The platform owner uses the same API and may create keys for their own apps too.

Never embed platform secrets, database credentials or S3 credentials in mobile apps or browser bundles.

## Architecture
Developer apps -> HTTPS -> Go API -> PostgreSQL metadata + SeaweedFS S3 object storage.

The software and API can be owned by you, but file bytes physically live on the disks of the servers where SeaweedFS is deployed. This can later move to hardware you own.
