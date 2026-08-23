# HTTP API Package Guide

## Routes And Responses

- Keep list, download, browser, health, assets, and OAuth metadata routes public. Wrap only mutation routes with their exact scope.
- Use `internal/api` DTOs for JSON. Keep browser HTML, API JSON errors, and plain unknown-route 404s distinct.
- Build links with escaped `Path` plus `RawPath`. Downloads accept only basename-style filenames and regular files.
- The configured canonical URL, not request `Host` or forwarding headers, owns pacman links and OAuth metadata. Path-scoped resources publish RFC 9728 metadata below that path.
- Browser lists group package variants by name. For metadata architecture `any`, links still use the repository target architecture.

## Uploads And Auth

- Keep uploads streaming: bound the whole body with `http.MaxBytesReader`, iterate `MultipartReader`, and accept exactly file parts `package` plus optional `signature`. Never use `ParseMultipartForm`.
- Always defer staging cleanup. A positive option overrides the 500 MiB default; production read/write/idle timeouts also bound uploads.
- Missing auth configuration fails closed. Preserve Bearer challenge parameters and the distinctions among missing credentials, malformed requests, invalid tokens, and insufficient scope.
- Templates/assets are embedded; templates parse eagerly with `template.Must`.

## Verification

- Tests use a real temporary repository service with fake repository commands; call `Service.Initialize` before constructing the handler.
- Run `go test ./internal/httpapi`.
- For HTML: `npx --yes htmlhint@1.7.0 --config .htmlhintrc 'internal/httpapi/web/*.html'`.
- For CSS: `npm install --no-save stylelint@16.24.0 stylelint-config-standard@39.0.0`, then `npx stylelint internal/httpapi/web/assets/app.css`. Do not lint vendored `pure-min.css`.
