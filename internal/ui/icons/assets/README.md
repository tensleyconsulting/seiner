SVG sources for product icons, rasterized to PNG in memory at startup.

Every file here is original artwork drawn for Seiner. None is copied or
traced from Google's product icons or any other icon set. Keep it that way:
do not add vendor icons or logos. Users who want those load them with
`--icons-dir` (see the README).

Each file is named after its `model.ServiceType`; `logo.svg` is the app
logo, `cursor.svg` the navigation pane cursor, `cloud-storage-folder.svg` a
folder (object prefix) inside a bucket, which `icons.For` picks for prefix
rows, and `group-folder.svg` a bucket node of a long list (a Workspace org
unit or a letter bucket), which it picks for bucket rows. The cursor is the one outline
icon, drawn in the theme accent (`#8AB4F8`) rather than a category palette,
so it reads as UI rather than as a resource. `TestEveryServiceHasSVG` fails when a service has no file here, and
`TestAssetConventions` when a file breaks a rule it can check (the grid,
plain attributes, `rx` with `ry`).

## Style

Flat, rounded, and chunky, so an icon still reads at 2x1 cells (about
18px). No outlines, gradients, shadows, or faces.

- A 24x24 `viewBox`, filled edge to edge less a margin of a unit or two.
  The logo alone is 32x24, to fill its wider header and intro grids.
- Nothing finer than about 2 units: a stroke, a gap, or a detail thinner
  than that blurs away at list size. Standalone strokes are 2 to 2.8 wide
  with round caps and joins; white detail lines on a filled shape can go
  down to 1.6.
- Round every corner: `rx` 2 to 4.5 on large shapes, about 1 on small ones.
- One palette per category, plus white for details on top of a filled shape
  and gold `#FFC93C` for small accents (prompts, seals, keys, sparkles).

| Category | base | light | deep |
|----------|------|-------|------|
| Compute (Compute Engine, GKE, Cloud Run, Functions), app logo | `#5B6CF6` | `#B3BCFF` | `#3A47C9` |
| Storage | `#16B3A3` | `#93E6DC` | `#0B857A` |
| Databases | `#1FA3D6` | `#9ADCF5` | `#10789F` |
| Analytics | `#9061F2` | `#D2BDFF` | `#6B3FCC` |
| Networking | `#34B26A` | `#A3E6BE` | `#1F8A4D` |
| Security and IAM | `#F06A50` | `#FFC0B0` | `#C9452D` |
| Developer tools | `#E0569A` | `#FFB8DA` | `#B43575` |
| Operations (Logging, Pub/Sub, Service Usage) | `#7286AD` | `#C9D3EA` | `#4F5F82` |
| Resource hierarchy (organization, folder, project) | `#F2B33D` | `#FFE3A3` | `#C98A12` |

The base color carries the silhouette, so the icon shows on dark and light
terminals alike. Light goes on top of base, or on large secondary shapes.
A small shape standing on its own uses the mid tone (halfway between base
and light) instead, because light alone fades on a white background. Deep
is for small parts that sit on the edge of the silhouette: a pole, a foot,
a gear badge.

Within a family, related icons share a motif: certificates are a card with a
gold seal, policies are a page, perimeters are dashed, and child resources
reuse the product's shape with one change.

## What the rasterizer needs

Icons are drawn with `github.com/srwiley/oksvg`; `raster.go` scales
strokes, which oksvg otherwise draws in output pixels. Beyond that:

- Give a rounded `rect` both `rx` and `ry`. oksvg ignores `rx` alone.
- Fills are always nonzero. A hole inside a path must wind against its
  outline.
- Avoid an arc that is exactly a half circle (`a5 5 0 0 1 10 0`). oksvg can
  collapse it into a slash; use a radius a hair larger, or two quarter arcs.
- No `<style>`, `class`, masks, clip paths, filters, or images. Plain fill and
  stroke attributes only. A transform works on one element or group; nested
  transforms do not compose.

To check a change, draw it at list size: rasterize at 128px, scale to 18px,
and look at it on both a dark and a light background.
