
**Navigation callbacks**
- `on_load_start(url)` — fires when a navigation begins, good for showing a spinner
- `on_load_finish(url)` — fires when the page fully loads, hide spinner, update address bar
- `on_load_error(url, error_code, description)` — show an error page
- `on_url_changed(url)` — fires on every URL change including pushState/fragment, keeps address bar in sync
- `on_title_changed(title)` — page's `<title>` changed, update tab label
- `on_favicon_changed(url)` — favicon URL changed, fetch and display it

**Navigation control**
- `can_go_back()` / `can_go_forward()` — enable/disable nav buttons
- `go_back()` / `go_forward()`
- `reload()` / `stop()`
- `get_url()` — query current URL synchronously

**New window / popup policy**
- `on_new_window_requested(url) → Policy` — return `Allow` (open in new tab), `Deny`, or `Redirect` (load in same view); essential for target=_blank and window.open()

**Download handling**
- `on_download_requested(url, suggested_filename, mime)` — intercept downloads rather than letting them silently fail

**Permission callbacks**
- `on_permission_requested(type) → bool` — camera, microphone, notifications; return true to grant
- `on_geolocation_requested(origin) → bool`

**Find in page**
- `find(query, case_sensitive, wrap)` — highlight matches
- `find_next()` / `find_prev()`
- `on_find_result(match_index, total_matches)`
- `stop_find()`

**JavaScript / eval with result**
- `eval_with_result(js, callback<string>)` — async version of eval that returns the result back to Go, useful for scraping or testing

**Zoom**
- `set_zoom(factor)` — e.g. 1.0 = 100%, 1.5 = 150%
- `get_zoom() → float`

**Context menu**
- `on_context_menu(x, y, MediaType, link_url, selected_text)` — lets the shell draw a native context menu with the right items

**Auth / certificates**
- `on_auth_challenge(host, realm) → (username, password)` — handle HTTP Basic auth dialogs natively
- `on_certificate_error(url, error) → bool` — return true to proceed anyway (with user confirmation)
