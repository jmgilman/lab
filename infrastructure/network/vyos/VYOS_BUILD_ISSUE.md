# VyOS Build Failure Investigation

**Date:** 2025-12-27
**Branch:** `test/update-vyos-tests`
**Issue:** VyOS image build failing with unmet apt dependencies

## Problem Statement

The GitHub Actions workflow for building VyOS images is failing during the `build-container` job with the following error:

```
The following packages have unmet dependencies:
 vyos-1x : Depends: fuse-overlayfs but it is not going to be installed
           Depends: frr (>= 10.2) but 8.4.4-1.1~deb12u1 is to be installed
           Depends: keepalived (>= 2.0.5) but it is not going to be installed
           Depends: podman (>= 4.9.5) but it is not going to be installed
E: Unable to correct problems, you have held broken packages.
```

The `vyos-1x` package requires FRR >= 10.2, but apt is installing FRR 8.4.4 from Debian's repository instead of the VyOS repository.

## Investigation Timeline

### Initial Hypothesis: Git Commit Pinning Mismatch

The workflow was pinning the vyos-build repository to commit `3d67842c4562fb2b2694416c1d4671fb83da08e1` while using the `vyos/vyos-build:current` Docker image. We hypothesized this caused a version mismatch.

**Fix Attempted:** Removed git checkout pinning to use latest vyos-build with latest Docker image.

**Result:** Same dependency error persisted.

### Second Hypothesis: Missing Mirror Configuration

VyOS official nightly builds explicitly pass `--vyos-mirror`, `--debian-mirror`, and `--debian-security-mirror` flags.

**Fix Attempted:** Added all three mirror flags:
```bash
--vyos-mirror https://packages.vyos.net/repositories/current/
--debian-mirror http://deb.debian.org/debian/
--debian-security-mirror http://deb.debian.org/debian-security
```

**Result:** Same dependency error persisted.

### Third Hypothesis: Stale apt Cache

The container's apt cache might be outdated.

**Fix Attempted:** Added `apt-get update` before the build command.

**Result:** Same dependency error persisted.

## Root Cause Analysis

### VyOS Package Repository Investigation

We queried the VyOS package repository directly:

```bash
curl -s "https://packages.vyos.net/repositories/current/dists/current/main/binary-amd64/Packages.gz" \
  | gunzip | grep "^Package: frr$"
# Returns: nothing - FRR package not found

curl -s "https://packages.vyos.net/repositories/current/dists/current/main/binary-amd64/Packages.gz" \
  | gunzip | grep "^Package:.*frr"
# Returns only: frr-exporter (not the main frr package)
```

**Finding:** The VyOS package repository at `packages.vyos.net/repositories/current` does not contain the main `frr` package. It only has `frr-exporter`. This is why apt falls back to Debian's FRR 8.4.4 instead of VyOS's FRR 10.2.

### VyOS Nightly Builds Comparison

VyOS official nightly builds ARE succeeding (latest: 2025.12.26-0021-rolling). Key differences:

| Aspect | Our Build | VyOS Nightly |
|--------|-----------|--------------|
| Runner | `warp-ubuntu-latest-x64-8x` | `self-hosted` |
| Build method | `docker run -v` with volume mount | `container:` directive (runs inside container) |
| Flavor | Custom `gateway` flavor | `generic` flavor |

The official builds use self-hosted runners which may have:
- Different network access to internal VyOS package sources
- Cached packages not available to public runners
- Additional apt sources configured in the self-hosted environment

### VyOS December 2024 Update Context

From the [VyOS December 2024 blog post](https://blog.vyos.io/vyos-project-december-2024-update):

> "One of the biggest news in the rolling release is that we are ready to update FRR — our routing protocol stack — to the latest 10.2 release."

This suggests FRR 10.2 was recently introduced to VyOS rolling, which may explain why the public package repository hasn't been fully updated or synchronized.

## Commits Made During Investigation

1. `a179712` - fix(ci): use latest vyos-build to fix apt dependency issues
2. `5158272` - fix(ci): add --vyos-mirror flag to match official VyOS builds
3. `cacca5d` - fix(ci): refresh apt cache and add debian-mirror flag
4. `e5409e1` - fix(ci): add debian-security-mirror flag for correct security repo URL

## Failed Workflow Runs

| Run ID | Commit | Error |
|--------|--------|-------|
| 20546156206 | 6f7b027 (pinned commit) | Unmet dependencies |
| 20546251162 | a179712 (unpinned) | Unmet dependencies |
| 20546283747 | 5158272 (+ vyos-mirror) | Unmet dependencies |
| 20546324094 | cacca5d (+ apt-get update) | Security repo URL error |
| 20546345290 | e5409e1 (+ security-mirror) | Unmet dependencies |

## Conclusions

1. **This is an upstream VyOS issue** - The public VyOS package repository is missing the `frr` package (and possibly others)

2. **VyOS nightly builds work due to different infrastructure** - Self-hosted runners likely have access to packages we cannot access from public GitHub Actions runners

3. **No code-level fix available** - The issue is in VyOS's package distribution, not our workflow configuration

## Recommended Actions

### Short-term Workarounds

1. **Download pre-built VyOS nightly ISO** - Use releases from https://github.com/vyos/vyos-nightly-build/releases and convert to container image

2. **Skip VyOS build on PR** - Temporarily disable the `build-container` job until upstream is fixed

3. **Use cached/older VyOS image** - If a working container image exists, use it for testing

### Long-term Resolution

1. **Monitor VyOS repository** - Check if `frr` package appears in public repository
2. **Report to VyOS** - Open issue at https://vyos.dev about missing packages in public repository
3. **Consider alternative build approach** - Investigate running build on self-hosted runner or using VyOS's Jenkins-based build system

## Files Modified

- `.github/workflows/vyos-build.yml` - Multiple fix attempts (should be reverted or kept depending on decision)

## Reference Links

- VyOS Build Repository: https://github.com/vyos/vyos-build
- VyOS Nightly Builds: https://github.com/vyos/vyos-nightly-build
- VyOS Package Repository: https://packages.vyos.net/repositories/current/
- VyOS Issue Tracker: https://vyos.dev
