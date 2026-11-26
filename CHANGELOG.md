# Changelog

## [0.1.1](https://github.com/bbmumford/diststore/compare/v0.1.0...v0.1.1) (2026-11-25)


### Bug Fixes

* the seed loop logs a scan that fails, and an atomic write that fails also reports its temp file's close error ([191e472](https://github.com/bbmumford/diststore/commit/191e4724727b5174274fc1532292df5f1066ec34))

## [0.1.0](https://github.com/bbmumford/diststore/compare/v0.0.1...v0.1.0) (2026-11-25)


### Features

* VerifyWrappingFrom checks a wrapping record against the issuers the caller trusts, since VerifyWrapping proves only that a record is self-consistent ([9a81ec5](https://github.com/bbmumford/diststore/commit/9a81ec5dd05b1e43cccaeb789f72a7436fec3556))
