# Assets

HighJack's asset hierarchy and pipeline rules live in
[PROVENANCE.md](./PROVENANCE.md).

```
assets/
├── source/     authored masters (SVG logos, icons) — committed
└── processed/  derived artifacts (empty until rasters exist)
```

Fonts are not stored here: they ship as Fontsource npm packages so versions
stay pinned and licenses ride along with the dependency lockfile.
