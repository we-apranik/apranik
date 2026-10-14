# Add an API module

An API lets another program read and drive a running Apranik; see [APIs](../configuration/api/index.md).
The module value implements `spec.APIModule` (settings, `Info`, `NewAPIServer()`), and the
server it builds implements `spec.APIServer`:

- `Start(ctx, coreProvider)` binds its own listener, returns once it is listening, and keeps
  serving in goroutines it owns; a taken port is `Start`'s error;
- `Close()` stops serving, ends every open request and waits;
- everything it knows of the running core comes from `spec.APICoreProvider`, whose methods
  are small and separate; an API imports `spec` and nothing from core;
- a method that cannot answer returns a `spec.ErrAPI...` sentinel; map it with `errors.Is`.

Test the module with a fake `APICoreProvider` and no core. Register it with
`moduleRegistry.APIs().Register(<name>.NewAPIModule)`.
