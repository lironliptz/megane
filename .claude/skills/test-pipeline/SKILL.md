---
description: >
  Test the file upload → pipeline → result flow end-to-end from the terminal without a browser.
  Use when the user wants to verify the pipeline works, check LLM output shape, or debug a
  specific file type. Requires the server to be running (make run).
---

## What you do

Login, upload a file, poll status until done, print the result JSON.

## Procedure

1. Confirm the server is reachable:
   ```
   !curl -sf http://localhost:$PORT/health || echo "SERVER NOT RUNNING — run: make run"
   ```
   Stop and tell the user if it isn't up.

2. Read `PORT` from `.env` (default 8080). Read `ADMIN_EMAIL` / `ADMIN_PASSWORD` from `.env`.

3. Login and capture token:
   ```
   !TOKEN=$(curl -sf -X POST http://localhost:$PORT/auth/login \
     -H 'Content-Type: application/json' \
     -d '{"email":"$EMAIL","password":"$PASSWORD"}' | jq -r '.data.token')
   ```

4. Ask the user for a file path to upload, or use any small PDF/image/txt in the repo if they say "use any".

5. Upload:
   ```
   !curl -sf -X POST http://localhost:$PORT/api/files/upload \
     -H "Authorization: Bearer $TOKEN" \
     -F "files=@$FILE" | jq .
   ```
   Capture the returned project `id`.

6. Poll `/api/files/$ID/status` every 2 s until `status` is `complete` or `error` (max 60 s):
   ```
   !for i in $(seq 1 30); do
       R=$(curl -sf http://localhost:$PORT/api/files/$ID/status -H "Authorization: Bearer $TOKEN")
       S=$(echo $R | jq -r '.data.status')
       echo "$S"; [ "$S" = "complete" ] || [ "$S" = "error" ] && break
       sleep 2
   done
   ```

7. Fetch and pretty-print result:
   ```
   !curl -sf http://localhost:$PORT/api/files/$ID/result \
     -H "Authorization: Bearer $TOKEN" | jq .
   ```

8. Report: status, pipeline stages from the events array, and result shape. Flag any `error` stage with the message.

## Notes

- If `GEMINI_API_KEY` is blank the pipeline will error at the LLM stage — tell the user to set it.
- Read actual PORT/credentials from `.env` using the Read tool before running curl commands.
