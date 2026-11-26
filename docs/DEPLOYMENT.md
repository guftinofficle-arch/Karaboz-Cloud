# Deployment checklist

The Compose stack is for local development and private testing. Before public use:
1. Deploy on a supported Linux server or cluster with persistent volumes.
2. Put the API and dashboard behind a reverse proxy with HTTPS and secure headers.
3. Generate independent random values for JWT_SECRET, database password and S3 access credentials. Never reuse example secrets.
4. Restrict PostgreSQL and SeaweedFS ports to private networks. Only expose the public API and dashboard.
5. Configure SeaweedFS replication/volume placement and durable backups; test restoring object data and PostgreSQL metadata together.
6. Configure monitoring, alerting, request rate limits, upload policies, audit retention and key rotation.
7. Add email verification, password reset, abuse controls, customer support and privacy/retention policies before accepting public customers.
8. Test tenant isolation, revoked API keys, quota enforcement and restore procedures before production traffic.

JWT and S3 credentials must stay on the server. Mobile/web clients should use scoped project keys only where appropriate; for public web clients prefer short-lived restricted upload tokens through a trusted backend.


CI validation runs on pushes to main and pull requests. Review the Actions result before merging deployment changes.
