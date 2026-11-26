# Karaboz Cloud REST API (MVP)

Base URL: your deployed API origin followed by /v1. JSON requests use application/json; file upload uses a raw request body.

## Account
POST /v1/auth/register accepts {"display_name":"Developer","email":"dev@example.com","password":"a-long-password"}. POST /v1/auth/login accepts {"email":"dev@example.com","password":"a-long-password"}. Both return a JWT token. Send it as Authorization: Bearer TOKEN for dashboard operations.

## Projects and keys
GET /v1/projects lists only the signed-in user's projects. POST /v1/projects with {"name":"My App"} creates one. POST /v1/projects/PROJECT_ID/keys with {"name":"Backend key","permission":"read_write"} creates a scoped key. Permission is read_write or read_only. The full key is returned once; only its hash is stored. GET /v1/projects/PROJECT_ID/keys lists metadata. DELETE /v1/projects/PROJECT_ID/keys/KEY_ID revokes a key.

## Storage authentication
Send X-Karaboz-Key: kbc_live_... on storage API requests. Keys are scoped to one project. A read_only key cannot create buckets, upload or delete objects.

## Buckets
POST /v1/buckets with {"name":"uploads"} creates a bucket. GET /v1/buckets lists the project's buckets. Names must be lowercase and 3-63 characters with letters, digits and hyphens.

## Objects
- Upload: PUT /v1/objects?bucket=uploads&key=avatars/user-1.png with raw file bytes and a Content-Type header.
- List: GET /v1/objects?bucket=uploads
- Download proxy: GET /v1/objects/download?bucket=uploads&key=avatars/user-1.png
- Signed download URL: POST /v1/objects/presign?bucket=uploads&key=avatars/user-1.png returns a URL that expires after 15 minutes.
- Delete: DELETE /v1/objects?bucket=uploads&key=avatars/user-1.png
- Usage: GET /v1/usage

Initial release has a configurable single-request upload limit (25 MiB default, maximum 250 MiB in this build). Multipart/resumable upload is planned for a later release.

## Example
Create a bucket:
    curl -X POST https://YOUR_API/v1/buckets -H 'X-Karaboz-Key: YOUR_PROJECT_KEY' -H 'Content-Type: application/json' -d '{"name":"uploads"}'

Upload a file:
    curl -X PUT 'https://YOUR_API/v1/objects?bucket=uploads&key=hello.txt' -H 'X-Karaboz-Key: YOUR_PROJECT_KEY' -H 'Content-Type: text/plain' --data-binary @hello.txt

Never place write-enabled keys in public frontend bundles. For browser uploads, add short-lived upload tokens or a trusted backend proxy before public rollout.
