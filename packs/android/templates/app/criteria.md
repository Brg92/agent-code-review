# Review criteria: <REPOSITORY NAME> (app)

> **Setup.** Copy this file and `config.json` to `.agent-code-review/` in the
> repository and fill every `<...>` placeholder. Delete what does not apply.
> Write here **only what is specific to this repository**. The standard Android
> and Kotlin rules come from the android pack; copying them here creates two
> versions that drift. Merge this file like code: the criteria are a team
> agreement. A review always reads it from the target branch, so a change takes
> effect once it is merged.

In `config.json`, set `variants` to the build variants or flavors of the
repository, `disabledRules` to the pack rules that do not apply here, and
`dimensions` to fix a value that detection gets wrong, for example
`{ "ui-toolkit": "compose" }`. Pack rules cannot be weakened from a repository.

## Environment

<Shell, build commands and path conventions, for example:>

```text
./gradlew lint<Variant>
./gradlew test<Variant>UnitTest
```

Run them only on your own local work, never on someone else's branch: a build
runs whatever that branch defines, and the pipeline has already run the checks.

## Project facts

- Shared libraries this app consumes: <core-lib, feature-lib, ...>
- Build variants: <flavorA, flavorB, ...>, or "single variant"
- Conventions document: <path, for example `docs/conventions.md`>

## Project rules

Rules that exist only in this repository. Each one has a fixed kebab-case **id**
(a finding is recognised by it, so renaming an id makes old findings look new),
a **severity** and a **reason** that states the consequence. `BLOCKER` blocks
the change request; `MAJOR` and `MINOR` are tracked.

### Shared libraries and variants

- `<app>-library-contract-undeclared`, **MAJOR**. A change that depends on a new
  or changed API of <core-lib / feature-lib> lists the library change request
  under `## Related Changes`. Without it the app merges against a library
  version that does not exist yet.
- `<app>-variant-resource-missing`, **MAJOR**. A string, drawable or flag added
  under one of <flavorA / flavorB> exists in the others, unless the feature is
  declared variant-specific.

### Project conventions

- `<app>-analytics-direct-call`, **MAJOR**. Analytics events go through
  `<AnalyticsTracker>`, never the SDK directly: the tracker is the only place
  where consent is checked.
- `<app>-feature-flag-hardcoded`, **MAJOR**. Feature flags are read from
  `<FeatureFlags>`, never as literals: a hard-coded flag cannot be switched off
  in production.

## Known exceptions (never reported)

Exceptions to the rules of this file only. The standard exceptions are already
in the pack: `_binding!!` in the Fragment ViewBinding pattern, `kapt` instead of
KSP, and XML layouts with ViewBinding instead of Jetpack Compose.

- <for example: `LegacyFeatureBridge` is exempt from `<app>-analytics-direct-call`
  because it wraps the legacy SDK>
