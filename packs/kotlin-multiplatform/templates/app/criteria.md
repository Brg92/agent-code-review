# Review criteria: <REPOSITORY NAME> (Kotlin Multiplatform app)

> **Setup.** Copy this file and `config.json` to `.agent-code-review/` in the
> repository and fill every `<...>` placeholder. Delete what does not apply.
> Write here **only what is specific to this repository**. The standard Kotlin,
> Android and multiplatform rules come from the android and kotlin-multiplatform
> packs; copying them here creates two versions that drift. Merge this file like
> code: the criteria are a team agreement. A review always reads it from the
> target branch, so a change takes effect once it is merged.

In `config.json`, set `variants` to the build variants of the repository,
`dimensions` to fix a value that detection gets wrong, `ruleOptions` to change a
value a pack rule allows, and `disabledRules` to the rules of this file that are
switched off for now, and `sharedLibraries` to the project keys of the shared
libraries it consumes. Pack rules always apply and cannot be weakened from a
repository. Rules that name a platform apply per file: a file in `commonMain`
gets the Kotlin rules and the multiplatform rules, not the Android-only ones.

## Environment

<Shell, build commands and path conventions, for example:>

```text
./gradlew :shared:allTests
./gradlew :androidApp:lint<Variant>
```

Run them only on your own local work, never on someone else's branch: a build
runs whatever that branch defines, and the pipeline has already run the checks.

## Project facts

- Shared modules: <shared, ...>; native apps: <androidApp, iosApp>
- Targets: <android, iosArm64, iosSimulatorArm64, ...>
- How the iOS app consumes the shared code: <XCFramework, SwiftPM, CocoaPods>
- Conventions document: <path, for example `docs/conventions.md`>

## Project rules

Rules that exist only in this repository. Each one has a fixed kebab-case **id**
(a finding is recognised by it, so renaming an id makes old findings look new),
a **severity** and a **reason** that states the consequence. `BLOCKER` blocks
the change request; `MAJOR` and `MINOR` are tracked.

### Shared code

- `<app>-platform-code-in-common`, **MAJOR**. Platform behaviour is reached
  through `<PlatformServices>`, never from `commonMain` directly: common code
  must stay testable without a device.
- `<app>-ios-contract-undeclared`, **MAJOR**. A change to the public API that
  the iOS app calls lists the iOS change request under `## Related Changes`.
  Without it the shared module and the app merge out of step.

## Known exceptions (never reported)

Exceptions to the rules of this file only; an exception that names a pack rule is
ignored and reported, since pack rules always apply.

- <for example: `LegacyBridge` is exempt from `<app>-platform-code-in-common`
  because it wraps a platform-only SDK>
