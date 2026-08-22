#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034
# Hand-derived from WorkloadMaterializationHandler and EdgeWriter's canonical
# workload DEPENDS_ON write. The handler loads fact_records before writing and
# owns no shared projection intent, so the family keeps bespoke custom cells.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="workload_materialization"
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:DEPENDS_ON]->(target)"
IFA_FAMILY_PIN_SHARED_CELL=0
IFA_FAMILY_PIN_CELL_KIND="custom"
