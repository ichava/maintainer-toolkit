[← Docs index](../README.md#documentation)

# Icon-pack upstream tracking

*Reference + design notes.*

Ichava child icon packs vendor SVG assets from open-source upstream
sources (Twemoji, OpenMoji, lipis/flag-icons, Tabler, ...). Over time
those upstreams ship new versions and packs drift. This system lets
host apps **track**, **notify on**, and **execute** updates without
each pack reinventing the wheel.

## Design (one paragraph)

Each pack declares an `upstream` block in its `resources/assets/svg/config.json`.
The block names the source (`github`, `github-tag`, `npm`, `packagist`,
or a generic `url`), the version the pack currently vendors, the URL
the checker should GET to discover the latest version, the CDN URL
templates for end-users who don't want to vendor, and (optionally) a
command that refreshes the assets. The `IconPackUpdateChecker` service
in core reads every registered pack's block, hits each `version_check_url`,
parses the response per source type, compares with `current_version`,
and dispatches an `IconPackUpdateAvailable` event for every stale pack.
Responses are cached for 12 hours (configurable) to dodge GitHub's
60/hour unauthenticated rate limit. The CLI command `php artisan
ichava::ichava-core.check-updates` wraps the service for humans + CI.

## Why this shape

| Concern | How this design handles it |
|---|---|
| Distributed config | Each pack owns its upstream metadata. Core has zero per-pack knowledge. |
| Centralised execution | One service understands `github` / `npm` / `packagist` / `url` -- packs don't reimplement HTTP. |
| Rate limits | NPM and Packagist have no rate limit and are preferred over GitHub API where available. GitHub responses cached for 12 hours by default. |
| Pluggable parsers | New source type = one new branch in `parseLatest()`. No interface changes. |
| Notifications | Event-driven. Host app subscribes once and routes to Slack / email / GitHub issues / dashboards. |
| CDN access | Same `cdn` block doubles as documentation and machine-readable so other tooling can compose URLs without re-coding the template per pack. |

## Schema

```json
{
    "upstream": {
        "source": {
            "type": "npm | github | github-tag | packagist | url",
            "package": "@twemoji/svg",       // type=npm
            "owner": "jdecked",              // type=github | github-tag
            "repo": "twemoji",               // type=github | github-tag
            "vendor": "tabler",              // type=packagist
            "version_field": "dist-tags.latest"  // type=url
        },
        "current_version": "17.0.0",
        "version_check_url": "https://registry.npmjs.org/@twemoji/svg/latest",
        "license": "CC-BY 4.0",

        "cdn": {
            "jsdelivr": "https://cdn.jsdelivr.net/npm/@twemoji/svg@{version}/{codepoint}.svg",
            "unpkg":    "https://unpkg.com/@twemoji/svg@{version}/{codepoint}.svg",
            "github_raw": "https://raw.githubusercontent.com/jdecked/twemoji/v{version}/assets/svg/{codepoint}.svg"
        },

        "update_command": {
            "_note": "Dispatched by ichava/maintainer-toolkit. `recipe` references a module under src/ichava_maintainer_toolkit/recipes/. Use `type: npm` (no recipe) for the common case where the pack just mirrors one npm package.",
            "type": "recipe",
            "recipe": "emoji-sets"
        }
    }
}
```

### Supported source types

| Type | Endpoint shape | Latest version parsed from | Best for |
|---|---|---|---|
| `npm` | `https://registry.npmjs.org/<package>/latest` | `.version` | Vendors that publish to npm (Twemoji, lipis flag-icons, Tabler, Heroicons, Lucide, Bootstrap Icons, ...). **No rate limit.** Preferred. |
| `github` | `https://api.github.com/repos/<owner>/<repo>/releases/latest` | `.tag_name` | Vendors that cut GitHub releases. 60/hr unauthenticated; cached 12h. |
| `github-tag` | `https://api.github.com/repos/<owner>/<repo>/tags?per_page=1` | `[0].name` | Vendors that tag in git but never cut GitHub releases. |
| `packagist` | `https://repo.packagist.org/p2/<vendor>/<package>.json` | newest non-`dev-*` `version` | Composer-native icon vendors. |
| `url` | (user-supplied) | dot-path from `source.version_field` | Escape hatch for anything else. |

### Opting out

A pack with no `upstream` block in its `config.json` is reported as
`no-upstream` by the tracker and is otherwise ignored. This is the right
choice for vendored snapshots of commercial / closed-source assets where
no public registry exists to poll. `ichava/icon-sets-metronic` (commercial
KeenThemes assets, no public release feed) is the reference example.

## Notification hook

When the checker finds a stale pack, it dispatches:

```php
Simtabi\Laranail\Ichava\Events\IconPackUpdateAvailable
    ->packageName    string   "ichava/icon-sets-emoji"
    ->currentVersion ?string  "17.0.0"
    ->latestVersion  string   "17.1.0"
    ->releaseUrl     ?string  "https://github.com/.../releases/tag/v17.1.0"
    ->releaseNotes   ?string  (first ~2000 chars of the release body)
```

Subscribe in your host app:

```php
// app/Providers/EventServiceProvider.php
protected $listen = [
    \Simtabi\Laranail\Ichava\Events\IconPackUpdateAvailable::class => [
        \App\Listeners\NotifyTeamOfIconUpdate::class,
    ],
];
```

```php
// app/Listeners/NotifyTeamOfIconUpdate.php
public function handle(IconPackUpdateAvailable $event): void
{
    Notification::route('slack', config('slack.icon-updates'))
        ->notify(new IconPackHasUpdate($event));
}
```

## CLI

```bash
# Report on every registered pack
php artisan ichava::ichava-core.check-updates

# Restrict to one
php artisan ichava::ichava-core.check-updates --package=ichava/icon-sets-emoji

# Machine-readable for CI / cron
php artisan ichava::ichava-core.check-updates --format=json --fail-on-stale
```

`--fail-on-stale` exits non-zero when any pack is behind, so a
scheduled CI job can fail-and-notify on drift.

## Schedule it

```php
// app/Console/Kernel.php
$schedule->command('ichava::ichava-core.check-updates --fail-on-stale')
    ->dailyAt('03:00')
    ->withoutOverlapping()
    ->onOneServer();
```

## CDN-first workflow (skip vendoring)

If you don't want to ship 22MB of SVGs in your composer install, you
can read the pack's `cdn` block at runtime and serve from a CDN. A
typical helper:

```php
$cdn = config('ichava.icon-sets-emoji')['set']['cdn']['twemoji_jsdelivr'] ?? null;
$url = $cdn
    ? str_replace(['{version}', '{codepoint}'], [$version, $codepoint], $cdn)
    : null;
```

The pack's `codepoints.json` provides the slug -> codepoint mapping so
you only need a human-readable name (`grinning-face`) to compute the
final URL.

## Updating local assets

End users **don't** refresh assets locally -- Composer regenerates
`vendor/` on every install, so any local SVG change is discarded.
Asset refreshes run maintainer-side via
[`ichava/maintainer-toolkit`](https://github.com/ichava/maintainer-toolkit),
which polls each pack's `upstream` block, runs the matching recipe,
commits the refreshed SVGs back to the pack repo, and opens a PR. A
human reviews + tags a release; Packagist + `composer update` fans
the change out to every consumer.

See [`icon-pack-maintainer-sync.md`](maintainer-sync.md) for
the full architecture.

## See also

- [`architecture.md`](https://opensource.simtabi.com/documentation/ichava/core/architecture) -- where this slots into the
  overall service-provider hierarchy
  poisoning considerations for the version-check endpoints

---

[← Docs index](../README.md#documentation)
