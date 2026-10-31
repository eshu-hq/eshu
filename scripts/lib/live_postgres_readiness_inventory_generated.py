#!/usr/bin/env python3
"""Assembled PACKAGES for live-postgres-readiness (generated).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

from live_postgres_readiness_inventory_gen_cmd_bootstrap_index import TABLE as _TABLE_00
from live_postgres_readiness_inventory_gen_cmd_reducer import TABLE as _TABLE_01
from live_postgres_readiness_inventory_gen_cmd_webhook_listener import TABLE as _TABLE_02
from live_postgres_readiness_inventory_gen_internal_coordinator import TABLE as _TABLE_03
from live_postgres_readiness_inventory_gen_internal_query import TABLE as _TABLE_04
from live_postgres_readiness_inventory_gen_internal_query_admin import TABLE as _TABLE_05
from live_postgres_readiness_inventory_gen_internal_query_admin_store import TABLE as _TABLE_06
from live_postgres_readiness_inventory_gen_internal_query_iac import TABLE as _TABLE_07
from live_postgres_readiness_inventory_gen_internal_query_search_unscoped import TABLE as _TABLE_08
from live_postgres_readiness_inventory_gen_internal_query_semanticsearch import TABLE as _TABLE_09
from live_postgres_readiness_inventory_gen_internal_query_supply_chain import TABLE as _TABLE_10
from live_postgres_readiness_inventory_gen_internal_query_supply_chain_impact import TABLE as _TABLE_11
from live_postgres_readiness_inventory_gen_internal_reducer_freshness_links import TABLE as _TABLE_12
from live_postgres_readiness_inventory_gen_internal_reducer_status_summary import TABLE as _TABLE_13
from live_postgres_readiness_inventory_gen_internal_runtime import TABLE as _TABLE_14
from live_postgres_readiness_inventory_gen_internal_storage_postgres_part1 import TABLE as _TABLE_15
from live_postgres_readiness_inventory_gen_internal_storage_postgres_part2 import TABLE as _TABLE_16
from live_postgres_readiness_inventory_gen_internal_storage_postgres_activation import TABLE as _TABLE_17
from live_postgres_readiness_inventory_gen_internal_storage_postgres_backfill_targeted import TABLE as _TABLE_18
from live_postgres_readiness_inventory_gen_internal_storage_postgres_code_reachability import TABLE as _TABLE_19
from live_postgres_readiness_inventory_gen_internal_storage_postgres_config_artifacts import TABLE as _TABLE_20
from live_postgres_readiness_inventory_gen_internal_storage_postgres_infra_inventory import TABLE as _TABLE_21
from live_postgres_readiness_inventory_gen_internal_storage_postgres_maintenance import TABLE as _TABLE_22
from live_postgres_readiness_inventory_gen_internal_storage_postgres_membership import TABLE as _TABLE_23
from live_postgres_readiness_inventory_gen_internal_storage_postgres_poison import TABLE as _TABLE_24
from live_postgres_readiness_inventory_gen_internal_storage_postgres_retention_work_items import TABLE as _TABLE_25
from live_postgres_readiness_inventory_gen_internal_storage_postgres_search_index_retention import TABLE as _TABLE_26
from live_postgres_readiness_inventory_gen_internal_storage_postgres_status_summary import TABLE as _TABLE_27
from live_postgres_readiness_inventory_gen_internal_storage_postgres_vulnerability import TABLE as _TABLE_28
from live_postgres_readiness_inventory_gen_internal_storage_postgres_webhook import TABLE as _TABLE_29
from live_postgres_readiness_inventory_gen_internal_storage_postgres_webhook_actions import TABLE as _TABLE_30


PACKAGES = {}
for _table in (
    _TABLE_00,
    _TABLE_01,
    _TABLE_02,
    _TABLE_03,
    _TABLE_04,
    _TABLE_05,
    _TABLE_06,
    _TABLE_07,
    _TABLE_08,
    _TABLE_09,
    _TABLE_10,
    _TABLE_11,
    _TABLE_12,
    _TABLE_13,
    _TABLE_14,
    _TABLE_15,
    _TABLE_16,
    _TABLE_17,
    _TABLE_18,
    _TABLE_19,
    _TABLE_20,
    _TABLE_21,
    _TABLE_22,
    _TABLE_23,
    _TABLE_24,
    _TABLE_25,
    _TABLE_26,
    _TABLE_27,
    _TABLE_28,
    _TABLE_29,
    _TABLE_30,
):
    for _package, _files in _table.items():
        PACKAGES.setdefault(_package, {}).update(_files)
