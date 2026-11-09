# Review criteria: <LIBRARY NAME> (shared library)

> **Setup.** Copy this file and `config.json` to `.agent-code-review/` in the
> repository and fill every `<...>` placeholder. Delete what does not apply.
> Write here **only what is specific to this library**. The standard rules and
> the library contract rules (breaking changes, independence from the host,
> exposed dependencies) come from the android pack for the `library` profile.
> Merge this file like code: the criteria are a team agreement. A review always
> reads it from the target branch.

In `config.json`, `profile` is `library`, so the library rules apply. Set
`variants`, `dimensions`, `ruleOptions`, `disabledRules` and `sharedLibraries`
as for an app; pack rules always apply and cannot be weakened from a repository.

## Environment

```text
./gradlew :<module>:lint
./gradlew :<module>:testDebugUnitTest
```

Run them only on your own local work, never on someone else's branch.

## Library facts

- Consumers: <com.example.app, com.example.feature, ...>, the repositories that a
  breaking change breaks.
- Public API: <packages that form the contract, for example `com.example.core.api.*`>;
  everything else is `internal`.
- Versioning: <semantic versioning; where the version is declared>.
- Release and publication: <how and where the artifact is published>.

## Project rules

Rules that exist only in this library. Each one has a fixed kebab-case **id**, a
**severity** and a **reason** that states the consequence. `BLOCKER` blocks the
change request; `MAJOR` and `MINOR` are tracked.

### Public contract

- `<lib>-api-outside-public-package`, **MAJOR**. New public symbols live under
  `<com.example.core.api>`; anything outside it is `internal`. Consumers must be
  able to tell the contract from the implementation by the package alone.
- `<lib>-changelog-missing`, **MINOR**. A change to the public API adds an entry
  to `<CHANGELOG.md>`, so consumers know what to adapt when they bump the version.

### Host integration

- `<lib>-theme-hardcoded`, **MAJOR**. Colours and typography come from the host
  theme attributes `<?attr/...>`, never literals: the same library ships in
  several brands.
- `<lib>-feature-flag-default`, **MAJOR**. A new feature flag defaults to off;
  the host turns it on.

### Versioning

- `<lib>-version-location`, **MINOR**. The version changes only in
  `<gradle.properties>`.

## Known exceptions (never reported)

Exceptions to the rules of this file only. The standard exceptions are already
in the pack: `_binding!!` in the Fragment ViewBinding pattern, `kapt` instead of
KSP, and XML layouts with ViewBinding instead of Jetpack Compose.

- <for example: `LegacyBridge` under `<com.example.core.legacy>` is exempt from
  `<lib>-api-outside-public-package` for the Java consumer>
