# Asset Provenance & Licensing

Every asset in this repository is either **authored in-repo** (MIT, same as
the code) or a **vendored open-license font** installed through Fontsource.
No third-party game assets are used anywhere.

## Authored assets

| Path                                    | Description                                                                                            | License         |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ | --------------- |
| `assets/source/branding/logo-mark.svg`  | HighJack mark (chip + rising card), full color on transparent                                          | MIT (this repo) |
| `assets/source/branding/logo-badge.svg` | Mark on rounded felt badge; source of `apps/web/public/favicon.svg` and `apps/game/public/favicon.svg` | MIT (this repo) |
| `assets/source/icons/*.svg`             | UI icon set: chip, dice, card, crown, bolt, rise — 24×24 grid, 2px strokes                             | MIT (this repo) |
| `apps/web/src/components/Logo.astro`    | Inline copy of the mark for SSR-friendly site rendering                                                | MIT (this repo) |

The mark's geometry is duplicated between the standalone SVGs and
`Logo.astro` intentionally (inline SVG avoids extra requests and inherits
theming). Keep them in sync when changing the mark.

## Vendored fonts (via npm packages)

Installed as npm dependencies of `apps/web` / `apps/game`; no font binaries
are committed directly to `assets/`.

| Package                                    | Family usage                           | License     |
| ------------------------------------------ | -------------------------------------- | ----------- |
| `@fontsource-variable/bricolage-grotesque` | Display/headings (`--hj-font-display`) | SIL OFL 1.1 |
| `@fontsource-variable/space-grotesk`       | Body/UI text (`--hj-font-body`)        | SIL OFL 1.1 |
| `@fontsource/space-mono`                   | Numerals/counters (`--hj-font-mono`)   | SIL OFL 1.1 |

Fontsource packages pin upstream Google Fonts sources; see each package's
`LICENSE` inside `node_modules` for the verbatim license text.

## Pipeline

```
assets/source/**        authored SVG masters (committed)
     │  manual export / future script
assets/processed/**     derived formats (WebP/AVIF rasters, optimized SVG)
     │
apps/*/public/**        deployment-ready files served at site root
```

Rules:

- **SVG** for logos, icons, and any flat UI graphic. Optimize before
  committing if exported from a design tool (strip editor metadata).
- **WebP/AVIF** only for illustrations/photos once they exist; keep source
  masters under `assets/source/`.
- No asset over ~200 KB may be committed without an explicit note here.
- Rasters generated _from_ sources are reproducible artifacts — prefer
  generating during build/deploy over committing them.
