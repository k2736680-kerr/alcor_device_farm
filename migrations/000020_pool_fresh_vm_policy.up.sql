-- ADR-0029 / DF-038 made long-lived device release non-destructive: releasing a
-- reservation only frees occupancy and deliberately does NOT queue a rebuild.
-- Some Alcor automation projects require the opposite guarantee: every run must
-- start from a factory-fresh emulator. That requirement is stricter, not
-- universally better (a rebuild costs a full container delete + recreate + cold
-- boot), so it is opt-in per Pool and defaults to the existing DF-038 behavior.
--
-- When fresh_vm_per_run is true, closing a reservation routes an Android
-- emulator through lifecycle_status='recycling' and marks the device
-- lifecycle_mode='rebuild'. lifecycle_mode is the durable authorization that
-- lets the warm-pool recycler destroy and recreate the container and its data
-- volume; without it a device would sit in 'recycling' forever.
ALTER TABLE device_pools
    ADD COLUMN fresh_vm_per_run boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN device_pools.fresh_vm_per_run IS 'Opt-in per-pool policy: when true, releasing an Android emulator reservation queues a destructive recycle (delete + recreate + cold boot) so the next run starts from a fresh VM. Default false preserves DF-038 non-destructive release.';