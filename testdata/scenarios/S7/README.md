# S7: change-request checks

Two pull requests that exercise the detectors that see the whole change request: missing tests, variant asymmetry, source-set asymmetry and `expect` without `actual`. They read the repository tree at the head commit, recorded in `testdata/github/S7/`, and, for `expect`/`actual`, a code search.

| Change request | Branch → base | Pull request |
| --- | --- | --- |
| sample-app | `feat/data-s7-missing-tests-and-variants` → `chore/tests-s7-existing-test-base` | [sample-app#8](https://github.com/acr-plugin-sandbox/sample-app/pull/8) |
| kmp-shared | `feat/shared-s7-device-model` → `main` | [kmp-shared#3](https://github.com/acr-plugin-sandbox/kmp-shared/pull/3) |

Both pull requests are opened by the author account and never merged. The sample-app one targets a base branch that holds only `GreetingHelperTest.kt`, written first: the test is in the repository tree but not in the change request, so only the tree can tell that the test exists. The base branch has the same criteria as `main`: `.agent-code-review/config.json` with profile `app`, pack `android` and `variants` `["free", "paid"]`. kmp-shared targets `main` because code search only indexes the default branch, and `main` has no criteria files (S6 D).

## sample-app#8

Paths are under `app/src/`.

| File | Change | Expected |
| --- | --- | --- |
| `main/java/com/example/sample/data/AvatarMapper.kt` | new class, no test anywhere | `missing-tests`, positive |
| `main/java/com/example/sample/data/GreetingHelper.kt` | new class; `test/java/com/example/sample/data/GreetingHelperTest.kt` is in the tree of the base branch | no signal, negative |
| `free/java/com/example/sample/FeatureFlags.kt` | adds `SHOW_PROMO_CARD`; `paid/.../FeatureFlags.kt` exists and is unchanged | `variant-asymmetry` |

The new declarations have KDoc, so that no file-level signal hides these.

## kmp-shared#3

Paths are under `shared/src/`.

| File | Change | Expected |
| --- | --- | --- |
| `androidMain/kotlin/com/example/shared/Platform.android.kt` | adds the manufacturer to `platformName()`; `iosMain/.../Platform.ios.kt` exists and is unchanged | `kmp-source-set-asymmetry`; a platform name is meant to differ, so the judgement dismisses it |
| `commonMain/kotlin/com/example/shared/DeviceInfo.kt` | new `expect fun deviceModel()` | `kmp-expect-without-actual` for iOS |
| `androidMain/kotlin/com/example/shared/DeviceInfo.android.kt` | the `actual` for Android | none |

The asymmetry rules do not treat `commonMain` as a source set: the asymmetry is between `androidMain` and `iosMain`, and the `expect` in `commonMain` is the other check.

## Change-request signals

The detectors run on the recorded tree and the recorded code search. A scenario test keeps this table equal to what they produce.

| Change request | Rule | File | Result |
| --- | --- | --- | --- |
| `sample-app#8` | `variant-asymmetry` | `FeatureFlags.kt` | needs judgement: confirmed MAJOR |
| `sample-app#8` | `missing-tests` | `AvatarMapper.kt` | automatic MAJOR, not dismissed |
| `kmp-shared#3` | `kmp-source-set-asymmetry` | `Platform.android.kt` | needs judgement: dismissed |
| `kmp-shared#3` | `kmp-expect-without-actual` | `DeviceInfo.kt` | not verified: `search-unavailable` |

### The search of `deviceModel`

The detector looks for an `actual` of `deviceModel` for iOS and needs a code search of the target branch. GitHub's code search does not serve the sandbox repositories: for `deviceModel repo:acr-plugin-sandbox/kmp-shared` it returned `total_count` 0 and `incomplete_results` true, as it did for every sandbox search, so the recording holds no file. An empty incomplete result proves nothing, so the detector reports the file as not verified (`search-unavailable`) and raises no signal. The last row of the table is that case.

With a working index the search finds nothing, since `deviceModel` does not exist on `main`, and the detector signals the `expect` (`s2`, BLOCKER): the Android `actual` is in the change request, and iOS has files (`Platform.ios.kt`) but no `actual`.

The hand-written judgement follows what the product builds from the recordings, so it decides only `s1` (dismissed) and has nothing to say about the `expect`. The scenario test shows the other outcome: it answers the search with a complete empty result, which gives `s2`, and adds the confirmation of `s2` (BLOCKER) to the judgement.

## Expected verdicts

- **sample-app#8:** OK. Two MAJOR findings: the variant asymmetry and the missing test of `AvatarMapper`.
- **kmp-shared#3, from the recordings:** OK. There is no signal for the `expect`, and `DeviceInfo.kt` is not verified (`search-unavailable`); the only signal, the asymmetry, is dismissed.
- **kmp-shared#3, with a working search:** BLOCKED, because of the missing iOS `actual`: `s2` is raised and confirmed.
