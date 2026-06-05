## Learnings: GUI Login/Register View

### Fyne Patterns
- `widget.NewPasswordEntry()` creates a password-masked Entry (replaces `Entry{Password: true}`)
- `widget.DangerImportance` is the Fyne v2.7.x equivalent of error styling
- `widget.HighImportance` gives buttons accent color
- `fyne.Do(func())` is the correct way to update UI from goroutines
- `test.NewApp()` creates a headless app for testing but still requires X11 libraries at link time

### Auth Service Integration
- `auth.AuthService.Login(ctx, username, password) (*domain.User, error)`
- `auth.AuthService.Register(ctx, username, password) (*domain.User, error)`
- Both use `storage.Initialize(":memory:")` for test databases
- Both expect `context.Context` as first argument (use `context.Background()`)

### Design Decisions
- Mutex guard prevents double-submit without setting loading state prematurely
- Validation happens BEFORE `setLoading(true)` so failed validation doesn't lock the UI
- `container()` method is called per-render, but `Refresh()` just re-layouts — dynamic content needs field-level updates
- Form title "Sign In" is static; register mode doesn't change it (acceptable for MVP)
