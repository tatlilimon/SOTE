## Issues: GUI Login/Register View

### Tests Cannot Link (Environment)
All Fyne GUI tests fail at link time: `cannot find -lXxf86vm`. This is a system dependency (libxxf86vm-dev) missing on the build machine. The existing `app_test.go` and `theme_test.go` have the same issue.

- **Severity**: Blocks test execution only; code compiles and vets clean
- **Fix**: `apt-get install libxxf86vm-dev` (requires root)
- **Verification**: `go build` and `go vet` pass; auth integration tests pass
