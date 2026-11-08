# Review criteria: <LIBRARY NAME> (Kotlin Multiplatform library)

> **Setup.** Copy this file and `config.json` to `.agent-code-review/` in the
> repository and fill every `<...>` placeholder. Delete what does not apply.
> Write here **only what is specific to this library**. The standard rules, the
> library contract rules (breaking changes, independence from the host, exposed
> dependencies) and the multiplatform rules come from the android and
> kotlin-multiplatform packs for the `library` profile. Merge this file like
> code: the criteria are a team agreement. A review always reads it from the
> target branch.

In `config.json`, `profile` is `library`, so the library rules apply. Set
`variants`, `disabledRules` and `dimensions` as for an app; pack rules cannot be
weakened from a repository.

## Environment

```text
./gradlew :<module>:allTests
./gradlew :<module>:apiCheck
```

Run them only on your own local work, never on someone else's branch.

## Library facts

- Consumers: <com.example.app, the iOS app, ...>, the repositories that a
  breaking change breaks.
- Public API: <packages that form the contract, for example `com.example.core.api.*`>;
  everything else is `internal`.
- Targets: <android, iosArm64, iosSimulatorArm64, ...>
- Versioning: <semantic versioning; where the version is declared>.

## Project rules

Rules that exist only in this repository. Each one has a fixed kebab-case **id**,
a **severity** and a **reason** that states the consequence.

### Public contract

- `<lib>-swift-api-undeclared`, **MAJOR**. A new public type in `commonMain` states
  how Swift calls it: an iOS consumer cannot use what it cannot express.

## Known exceptions (never reported)

Exceptions to the rules of this file only.

- <for example: `internal.compat` is exempt from `<lib>-swift-api-undeclared`>
