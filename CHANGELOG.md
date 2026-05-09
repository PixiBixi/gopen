# Changelog

## [1.3.1](https://github.com/PixiBixi/gopen/compare/v1.3.0...v1.3.1) (2026-05-09)


### Refactoring

* **output:** extract buildOpenCmd/buildClipboardCmd for testability ([03d3a15](https://github.com/PixiBixi/gopen/commit/03d3a1543a125db1ef0fd0c0f4f2584b61bffc6b))
* **output:** inject lookPath into buildClipboardCmd for Linux testability ([13a52e9](https://github.com/PixiBixi/gopen/commit/13a52e9f6ef679b489538391cd6d36d9103fd30a))


### Build

* reduce binary size by 33% with -s -w -trimpath CGO_ENABLED=0 ([84aaac9](https://github.com/PixiBixi/gopen/commit/84aaac92a59fa1983102ddc822d3ec4f1f42f4f6))
