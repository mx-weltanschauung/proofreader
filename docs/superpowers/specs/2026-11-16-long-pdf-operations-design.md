# Long PDF operations: HTTP deadlines

Large work originals and page preview batches can take longer than the server's 15-second `ReadTimeout` and `WriteTimeout`. The handler may finish and commit while the reverse proxy receives a closed connection and returns 502. Repeating page creation then wastes work on pages already created.

Extend the connection deadlines only in `WorkHandler.UploadFile` and `WorkHandler.CreatePages`, using `http.ResponseController` through the existing middleware `Unwrap` chain. Allow nine minutes, below the nginx `/api/` response timeout of ten minutes. Upload extends both read and write deadlines before parsing the multipart body; page creation extends its write deadline before processing. Log failure to set a deadline. Keep the global server limits for all other routes.

Verify each handler attempts the intended deadline with a recording `ResponseWriter`, and run API, middleware, and build checks. Publication can resume from partial work through the existing idempotent importer; no migration or data rewrite is involved.
