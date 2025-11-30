# Changelog

## Unreleased

- Step 4d: updated series deletion to remove all immutable segment files for the deleted series while keeping legacy file filtering for compatibility.
- Step 4c: added background segment compaction that merges series with more than 10 immutable segments into a new checksummed segment via temp-file write and atomic rename, then removes compacted source segments.
- Step 4b: changed new storage flushes from rewriting the legacy whole-node `.parq` file to writing immutable per-series segment files under a node-specific segment directory. Reload reads both new segments and any legacy node file for compatibility.
- Step 4a: bumped the custom `.parq` storage format from version 2 to version 3 and added a CRC32 checksum over the columnar data section. Readers now reject checksum mismatches with a corrupt segment error instead of returning unchecked data.
