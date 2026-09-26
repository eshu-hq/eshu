// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"fmt"
	"path"
	"strings"
	"testing"
)

// migrationShippedChecksums pins the sha256 of every shipped migration
// file's SQL bytes, keyed by filename. It is the per-file companion to
// goldenBootstrapDefinitionsDigest in embed_invariant_test.go: that single
// combined hash catches any drift across the whole set but its failure
// message does not say which file changed. validateMigrationManifest below
// names the exact file.
//
// #7002: 093_cross_scope_completion_queue.sql shipped, was edited in place
// twice (#6785, #6923), and was restored to these shipped bytes rather than
// tolerated as changed -- see checksum_alias.go and
// README.md. That incident is what this manifest exists to prevent from
// recurring silently.
//
// Adding a new migration appends its line here (run
// `go test ./internal/storage/postgres/migrations -run TestMigrationChecksumManifestPinsEveryShippedMigration -v`
// against the new file and copy its reported checksum). Never edit or remove
// an existing line: a shipped migration's bytes are immutable once applied to
// any real database. Widen it through a new guarded migration instead.
var migrationShippedChecksums = map[string]string{
	"001_ingestion_scopes.sql":                                               "054ebcb810faf78c693141834892781be1ce4d22268bed154c16bd5cc9e3dc1b",
	"002_scope_generations.sql":                                              "74a1776523f3a14f27b53e42dc0275248f5774fcc8d4a88bd769922514f29de7",
	"003_fact_records.sql":                                                   "17e98ba4f72588f0913d2ed631e6699c24e52958674b367ca4b9218c1f2a47d1",
	"003_service_catalog_fact_record_indexes.sql":                            "d457f0954990849d4ebb3ce3816de3fe12ec3af0597835539b56ee9d29665761",
	"003a_fact_record_sbom_attestation_indexes.sql":                          "e5aa0e3023d9ccaa6a960f9284b1d3f6215afc20251c491138e9f73f71dc53cd",
	"003b_eshu_search_index.sql":                                             "93480acd3d21b8da7053982b8aa603fdc06f78a0734053a08f8920ae2fe2e1e4",
	"003c_eshu_search_vector_metadata.sql":                                   "c6e381bbd3757bfe24ea083472bba6412aaf39a46706497ebc531ae5848024f0",
	"003d_eshu_search_vector_values.sql":                                     "96879e760cb0075c0b5320f75d6bfaf0847082cc43da936e82cde2dbb2a4707e",
	"004_content_store.sql":                                                  "236862da7ff45760fad77bd97d7bb82d323f0f3509e52032def81da77476b1cc",
	"005_fact_work_items.sql":                                                "1ec8e11fcab1e34bc5655e2a0e72f7d30181d3f3a81e737a85e22adf416b61df",
	"006_fact_work_item_audit.sql":                                           "b31d31cad7f7aad9ebc0362a76ec84243fcd668ab9c805b09f5c255f2a3c10d1",
	"006a_semantic_extraction_jobs.sql":                                      "2552856db5362a1d11bffaca54f5674b5aacf814cde1660910553fac93b369df",
	"006b_collector_generation_dead_letters.sql":                             "36be53827bbf052e18d23d60b91d3ed875397d1191479a9aa0eca4d381e81c01",
	"006b_governance_audit_events.sql":                                       "8660e65d71d05ace9193371e668efa7a0be375f0ae4b3949be37d4ce51a23f58",
	"006c_tenant_workspace_grants.sql":                                       "56809feceb69b3d19260c1d806992c0d32167dd90ee47ee61372cd06b67a7991",
	"006d_generation_retention_events.sql":                                   "09019aa15da75c6e14fa5cc37da89d07ff8f16310c065b06163e27a9dac90a15",
	"006d_scoped_api_tokens.sql":                                             "c8c6097dbfa7eb67e60a8a34b8df138aee480b0416e07221a966e4b904771580",
	"006e_identity_subjects.sql":                                             "e91b954879a36d00434fb45234a9c774745ffcd35a18ee6bf98fdeee2c9211e7",
	"006f_browser_sessions.sql":                                              "8eeb03a2e8796bb3274a37a2ac5e7dd4147a7a5417e4895612717cf2ded6e2f3",
	"006g_identity_oidc_login.sql":                                           "39c497a8111f34f0555d60195198926be02af33fba690a4e76ad22ab40fa485e",
	"007_projection_decisions.sql":                                           "e26be6ea797de0ac67fd9d499401d23d33413541fd2a1c2939e500d0fbbc66e9",
	"007a_admission_decisions.sql":                                           "d2004bb1953ce8a94576020708658f5e0248dea05d8defde9cb3e284bdccfb4e",
	"008_shared_projection_intents.sql":                                      "5b7357f41bb3756f82ca4a677b64dd16c03b41189fce49c1b3cef5d337bed6d6",
	"009_runtime_ingester_control.sql":                                       "65bc89dffd54e7579886fcda0a81b277f7fc256160e705045c9c796562806cc2",
	"010_relationship_tables.sql":                                            "be371c26ed9c8d07d25b528f727b781edd1b3b1c536434b73e636182abe290a3",
	"011_shared_projection_acceptance.sql":                                   "9c0a5f87c1e0087ea6e952e5f8c29744170e0cc21715039c08c1835ee205e9c2",
	"012_graph_projection_phase_state.sql":                                   "b62ab1b524ed08c28d231cded941d1c5ddd605ca1212073801ebb7e5f1477c6d",
	"013_graph_projection_phase_repair_queue.sql":                            "4ebdac09361d255e798f053e15b74cb403f9fca24125027577a241490b3b6a8c",
	"014_workflow_control_plane.sql":                                         "9be1e2752e027d9638c40edadc2c66be610b57b78a51fddfa31cb049c82a791a",
	"015_workflow_coordinator_state.sql":                                     "5037e9419c854f9e78d7e29d7cfc30d56b8f00996ae16843cdd43c8be9eb2a01",
	"016_deferred_maintenance_barriers.sql":                                  "5b0fda14d741e66eec79c15c7347a82aab499913269b8d88ac6518a59b96e262",
	"016_iac_reachability.sql":                                               "2fe477986c80c08349ce740974359f958eedcf6165c8534438b52e87be24ab11",
	"017_webhook_refresh_triggers.sql":                                       "704751513920912dc06ab972528e144456db1ebcaabc49018539b857886f69a1",
	"018_aws_pagination_checkpoints.sql":                                     "1b0c30c95509479d6b43ee4c2a7d2a4f52083c9f56ac9feae493f0bcd90e6765",
	"019_aws_scan_status.sql":                                                "8c837505aa994db10edbc3eee09900912def45076337ed9a8b8858e43f4eaf1e",
	"020_aws_freshness_triggers.sql":                                         "18aceca11da08731626c0dddff252f1c64371e7286ab8e23d66867bce22f4e16",
	"021_graph_schema_applications.sql":                                      "8cd80a1acb91f2e50e7b80ee8ff04ba3de0a9d7acff02c4a5227f49e721a307e",
	"022_vulnerability_source_states.sql":                                    "9e880bda4ed97cbf9dd4c42b9fd4811595d169699975796b824cde6dd48a70d8",
	"023_incident_freshness_triggers.sql":                                    "2cb7863e64c899c60ee64b2d93d43a9f6b53687ea57202ee502affa50a0a447d",
	"024_graph_endpoint_presence.sql":                                        "aa05d754e103f827eed6d5335797defc1d76b3726afd89245768e2b4c97093bb",
	"025_service_materialization_generations.sql":                            "d790722c6901d2424906e58cd65440e7b7bbfb401831bd5a505c36dc75acf409",
	"026_service_evidence_snapshots.sql":                                     "72b686f16657b264923f1a75a6c2f8278631626e5d986ed32215e80a88ad2667",
	"027_code_reachability.sql":                                              "b7c39d775a6236e09e7fe4330b39f674b7f4e5d140ec5fa961d109efbd290986",
	"028_function_summaries.sql":                                             "37873354ab93f9eb02f9835b82dd73352a1777e8d54020a7da0145e5c0ccaf87",
	"029_function_sources.sql":                                               "0b4044dde44228c8658ba8cf06ed9e862e63deda06fec46904c06bee591d27d0",
	"030_function_graph_ids.sql":                                             "5883b6a47ff679c76b2635a7545af9be4a2c68c951ccf4525e4651cf4b2155dd",
	"031_admin_replay_requests.sql":                                          "b1778d76d9d350979907ed7560049ba6f1102c803273a0f7ae8a758985caa2d9",
	"032_value_flow_fixpoint_components.sql":                                 "0adf4f2a3c3f7c1017d593086d7a4a28a9857adbf9237a142a92680fccb4b84e",
	"033_supply_chain_impact_canonical_winners.sql":                          "066ba0e5400bfbcf497b18ba19cfe8e235c838f424d452c6e3e8ca9d3f84154e",
	"034_supply_chain_impact_winners_materialization.sql":                    "e2d20c5830e1aeb5470cf121c32cb297b9fc88bb2577aeee4d20b0130b134049",
	"035_content_entities_repo_entity_idx.sql":                               "b65bff37f07ee3755112bb2e3991245e5148e352cf8a5428d12d5bd41f290130",
	"036_collector_evidence_summary.sql":                                     "172929c082e1acb8b8516ed14598fd47ba94702537a95b8fc1e4ce04f6d09ad0",
	"037_eshu_search_index_terms_doc_idx.sql":                                "aa80f8147aa5924c26d06318fd23cb6da9e6f7f173f1a77eba7988b2c9e3521f",
	"038_drop_eshu_search_index_terms_lookup_idx.sql":                        "52a9a460efc6b0c04040d5c5c3178a6cabbb717d3bbd2226fd0791d588f02975",
	"039_drop_eshu_search_index_terms_doc_idx.sql":                           "16912b55a86a71f63e0085f1b8aec76f6981463ca569b379a71f50b1307b4933",
	"039_gcp_freshness_triggers.sql":                                         "90233ec94cdbf5b34f78a3cff214e740bb8e113f5f4b9d47137642a82b77d800",
	"039a_partition_eshu_search_index_terms.sql":                             "ddaa96f8cbf2c781ec2f384ccb6a79cae188a08dbc9958804d2a2877e1909a13",
	"040_search_vector_build_materialization.sql":                            "bfb2a36f3798530110e2c37978c4f979c7302b4c4af4ba5fc41a00786b08cd93",
	"041_aws_gcp_freshness_claim_lease.sql":                                  "45190373899f3e5ef8816a839706206326fea821b44f401e81aa1c01a4fd75db",
	"042_deferred_backfill_partition_memo.sql":                               "141888636959445a8aa9d45a8f28328459e381bf9838bc3d7cd082a3bee87054",
	"043_dead_letter_poison_idx.sql":                                         "729f4a0544797944d97499a12d8374a3fa77d6f8397bfb3d7ee65919a0d38c76",
	"044_code_interproc_projected_edge.sql":                                  "23862cc5b65d01659798e5fda0fab6e355163d531d60fae38a77053f20f09222",
	"045_code_taint_evidence_projected_node.sql":                             "6cf97ececdec867159517becfc7676bc65d65c35a4e42ff9d1d9b88a98d742b3",
	"046_code_value_flow_backfill_state.sql":                                 "64f7de9fff7a3db9e0cf32e05b7b0951d42637d0efe2e28c498e1c7d7c2ff1c2",
	"047_projected_source_edge.sql":                                          "ba99e43d2eb29106e35af6495c633e27a4daed335846d6de90a71dea94539524",
	"048_drop_graph_projection_phase_state_lookup_idx.sql":                   "ef9996c936a0697ee77f582fc2d0e566dc9edb24a0081fb2fe6b45678478314d",
	"049_drop_fact_records_stable_key_idx.sql":                               "30b184c1367800803453999873a7b8957feb6abde02b0d9b3811e8abd9217ddf",
	"050_provider_config_sealed_secret.sql":                                  "cb058d8ba766d8905f3ccbb36209089d80cef77e476284e6c1bb5be58d69364c",
	"051_identity_bootstrap_credential.sql":                                  "f76e719dc9bc33ed933ec00c11b45b2a50bbeaad98d4afbdd5ddcadc517245fb",
	"052_identity_sign_in_policy.sql":                                        "96c45a7d8ce65e3696e472e78b4f6f16f33e4eb415b65956db117c7d1d65b872",
	"053_identity_local_credentials_must_change_password.sql":                "0b9b4755e923ade7c4b67330092693e2cbc9c064efa08fc54d56e527f04d9af9",
	"054_eshu_search_document_projection_state.sql":                          "1e209cf9517d6f52e854ae913f888008f0e488f1ee31e9a37e852a8ae8ad252d",
	"055_eshu_search_vector_scope_state.sql":                                 "c2644c353d61cf6db49cea42db5319f9d51877a57f667f47da2540a99f96da17",
	"056_graph_node_owner.sql":                                               "2de093c767ece4a2e7d48813a0e677a969c76ea3dd0961982340e2a147ae9435",
	"057_content_substring_index_state.sql":                                  "f0d10012b0bd3f727fe3a3af20cc58866dac6b22337ca0f43d85f6e2bd25d642",
	"058_relationship_reference_candidate_keys.sql":                          "f01100df3313b7ec5879deea5bb179feef52d2968048e25662a6f40eb5975730",
	"059_relationship_family_candidate_index.sql":                            "8e71e22334b8a53b30afa5bd8ff9d5a27c1ab18d3a4b9e925bc52bc428df15ba",
	"060_reducer_input_invalid_facts.sql":                                    "8104445aa81112cd018a0e8469882133bc43d0f62c8160b88096e4ab7e9ddb69",
	"061_identity_token_metadata_display_label.sql":                          "f0a714633aff26cd222b5aafcba959558c4bc84cea32c67810e1d3ed24643141",
	"062_content_entity_name_trgm_index.sql":                                 "f5e959e09bb63fdf650a5bfc7c4749d4d2345ea5f41d4a9db750a0d9c6b021f7",
	"063_identity_github_login.sql":                                          "83c8cde9b28f2d9ac9dca8cf0510e51dbfa1dc3ea73e1652b2dee5e99bdea142",
	"064_code_root_verdicts.sql":                                             "559d57c347e85172e24c094eff30290d6b56e2d41d8636a919fd4e850e2baf50",
	"065_create_documentation_findings_read_idx.sql":                         "3c6aaf70f96f6bbbc71543bfd71ee8be4aaf0a5d66587d07f6fefe4063da95d9",
	"066_create_documentation_findings_filter_idx.sql":                       "00bcc63e21741bd1f8144a7d92d4732e707c1773ea642ada6cf54f04aaf99186",
	"067_iac_active_inventory_index.sql":                                     "a6e056168336f6df7aa2638cec328fa09479b76398148083bae337ade7ebd08f",
	"068_drop_relationship_family_candidate_index_legacy.sql":                "ea19025ad3e27c5dd906144a94b6a703dbe3b1b40242dbf8d748975e3f15e369",
	"070_cloud_resource_owner_page_index.sql":                                "1ead12e3412e47a6078626a62117801ff080b50472a17e5912acff9c73bd0b52",
	"071_cloud_resource_owner_provider_page_index.sql":                       "dd7bf9f850af33ef19bc0e66c35945f9776fa7aad9b90f533139bff1ca2244a5",
	"072_cloud_resource_owner_region_page_index.sql":                         "3ee15533eecb5dbdcefb56362f2a32382e7a792136dd5adedcb84d090f97396f",
	"073_cloud_resource_owner_account_page_index.sql":                        "176bd308c343e03c707dc753e9c96c6bc0e87cd8047b74c27f87fe56851453ef",
	"074_graph_node_owner_backfill_state.sql":                                "e470cb54fe4d94c3c175d80413da00226c0e740eb18fc1ae648f30123e5c0501",
	"075_fact_records_active_container_image_slsa_idx.sql":                   "9f676bee600092b58f8f65519edcab64d3e195935fb158772be0efe8f8dc37ad",
	"075_kubernetes_live_pod_template_object_index.sql":                      "c7534fcdc187626be1f8b0ffba3bf4ea8b48eec4bd943cc5f5c7c2d8b2a16463",
	"076_crossplane_satisfied_by_redrive_state.sql":                          "d2a33e9b5afba0c9dd372813a3543cbd2ffc5f55beb69f5783b706f736bdc4e1",
	"077_content_entities_k8s_select_partial_index.sql":                      "2761d40c457cd6c2e10da3e6a685950a7b5dd01674813759a13f99de67490cc0",
	"078_cicd_run_watermarks.sql":                                            "263fb67adb47a07fd0b4f259cc47926187e84e16cfe818f1cba805ec9b3c6b4b",
	"079_supply_chain_runtime_filter_workload_index.sql":                     "95ad777975cbac342404af85509dfd21889938b5024f9410494418c607155683",
	"080_supply_chain_runtime_filter_entity_keys_index.sql":                  "ca86d8628f79f1e8f61129a23336870e40e50f5e99d4c293ef34f98a3fd752b9",
	"081_fact_records_active_container_image_ci_idx.sql":                     "3d6cd7fad61dcf814c41673295d4745a3526e348f3ebfcdf84511c2648c05563",
	"082_fact_records_active_container_image_ci_run_repository_idx.sql":      "375b8600c6ffb58592998db182c5dc9a016aba3f7d1a50270b35e4926c894287",
	"083_supply_chain_suppression_expiry.sql":                                "c57f53704e264908d0497d473dd1d3cc90c9c0da0a0e77cf06bcb23f1effde32",
	"084_supply_chain_impact_finding_id_index.sql":                           "9b21c9825fabfe253d9d172ccaef75683f3252c4189e983adab4da47ed36b0f3",
	"085_vulnerability_suppression_lineage_index.sql":                        "907e4e8e32a5363b2dca2009da40ef1fda2df239eac70d6e445df0a2b50d5473",
	"086_cloud_resource_owner_runtime_digest_index.sql":                      "a81361c49185f5b1e369b0d595195a8c82bd6017d4c633b9f5b98d4bc448a4c0",
	"087_aws_cloud_runtime_drift_write_admission.sql":                        "ee22edc0d748a7937f007fc717d6c4c585fab6b36de8f67831d8c276286f5696",
	"087_fact_records_active_oci_warning_idx.sql":                            "6e89b89e2a069f76259a87612dd492430ac4ad0d8f6afbdf887bd0c43e4433ea",
	"088_container_image_identity_cutover_guard.sql":                         "5e880031afdbdc9c3111cb07ca4c79cb40268835bcda55a476d7490349017770",
	"088_reducer_work_item_reopened_at.sql":                                  "21b0de129aeea5b901c164417011b271ede77af02068c5a9ec6e2a68edd0614a",
	"089_aws_cloud_runtime_drift_fencing_token_sequence.sql":                 "304fae99f67fda69a3ff9df9680b720b6694eed5d5184f077dd290a01714c18d",
	"090_fact_records_aws_cloud_runtime_drift_fencing_token_idx.sql":         "9982a0d3be48e3a53d8ad157c8eba29ebf16327752fd3d41791679341d4d4861",
	"091_ingestion_scopes_active_state_snapshot_index.sql":                   "a10fdb3bab8c2adb7ebfabb440951bae350721e4b4cbe490375535a5ae355cdb",
	"092_container_image_identity_support_store.sql":                         "51087593ff2ef2636a9296b74b4975fafd3f982233abcd5cee37cb1a407ed7e5",
	"092a_container_image_identity_support_current_view.sql":                 "011766e601f76cfd23d5c0ed765b72f416faff748bd252478ca26edf7d0cd5bd",
	"092b_container_image_identity_current_facts_function.sql":               "d45022900f9e8ef5028b26521a72d32ad8a4f390d8762dcb60ef73f2557229ce",
	"092c_container_image_identity_current_support_facts_function.sql":       "2cf905d5a3ce3552809eab72c78dfc7ff015b1d46c6c673f209d3fb61a5343a5",
	"093_cross_scope_completion_queue.sql":                                   "c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42",
	"095_cross_scope_completion_upgrade_seed.sql":                            "2dc5e0788a93b8996b455311d0032b5e8d7a154bfeafa43598441b6d0e6c85b6",
	"096_provenance_edge_identity_upgrade_seed.sql":                          "0a0923fc11d61ab76a8e107615877b1541f69aa7a3a5fb487bc967bbcdef1230",
	"097_container_image_identity_strength_precedence.sql":                   "cd627c7f1fb9ea13722ebf68813e87b69b29eb012056c15e881c995dbaf7e238",
	"098_shared_projection_unroutable_intents.sql":                           "94b84ef52c3e19aa5424d16ad415f4d61bdbc601f91f7a1352ce5f79e1f28b92",
	"099_fact_records_keyset_index.sql":                                      "be0207ec43cb85ee090cfa8eb24f9284d931a79c506a36e3b83eff32e5b38f3d",
	"101_code_reachability_entity_repository_scope_generation_idx.sql":       "d05c869fc30e9773588b79cf4af0f73eaf0801b0149d0a7572ad76e0900c61d2",
	"102_drop_code_reachability_entity_repository_idx.sql":                   "d02db74099ad22de56221c7b23253b8c76be312272ea7091aeddf899c6ad6f18",
	"103_code_reachability_entity_confidence_rank_idx.sql":                   "6ed43ed5c1519340b332e6ee77ae3122574fe64af8932b2ead599b855b0c0daa",
	"104_content_entities_language_type_idx.sql":                             "71951e38cb1740fc43178978b402ab234d7bed43a93d197814100ad242b4e668",
	"105_fact_records_identity_epoch_idx_v2.sql":                             "ee9513e5d7aa021c1b095c1fa7943b737363a483e7633ee6bc64187ba600f259",
	"106_drop_fact_records_identity_epoch_idx_legacy.sql":                    "20af4ebcafe3b90e5e1a2480bc6b9b206f58ea647fbe29c5cbd241518e561fd7",
	"107_content_entities_language_type_path_idx.sql":                        "d9d03dcbec673eeab64d6935782f1034155940682109b251b4f65e44759913a2",
	"108_shared_projection_generation_pending_index.sql":                     "7d413d037640fa752d76308bc5fa62c82dce1f642955518a53120616e1a24183",
	"109_infra_resource_entities.sql":                                        "c5c9d5f338eca77a594d38faabf9d9855069656931199421bd90d838212767e0",
	"110_reducer_readiness_waits.sql":                                        "62769246200e64d81b958dd5c003e0192fe2e2ce6e60c234f717e4ef4e1132d1",
	"111_code_function_fingerprint.sql":                                      "e3cb2e11f5be16a922280aadec7ee8a7a36efb7c3936fd58b5dc8f760c8dde15",
	"112_value_flow_refresh_producer_domains.sql":                            "2c20ac3e6bf3be3008d9b08bdfcae0a32358cff4d6b7938444500a048c8b6d21",
	"113_fact_work_items_cross_scope_source_v2_idx.sql":                      "9edb25c035a98b7a91c7d6a2005ab7ef6ff0a3371589f8fa20392bfbb6b9d3cf",
	"114_drop_fact_work_items_cross_scope_source_idx_legacy.sql":             "55862c8bcae2bb1703015484c98a0384094c69e8be0064010536a8dcb57c472f",
	"115_value_flow_refresh_global_seed.sql":                                 "801394d91de9c57b8c8a67a5175684ca1268aa14ed91ab928bf0beb22af20ef3",
	"116_value_flow_refresh_global_canonical_phase.sql":                      "cd41a402f2ff315dc7298db3928080756b5caafea2cacb4e4365bdb6ab976d14",
	"117_code_function_fingerprint_shingles.sql":                             "6ab84e81d1b25ddc859d609967823df2182d81c8bf749f32e394261b7520cd38",
	"118_cloud_resource_retract_liveness_index.sql":                          "84ed38d835b1f18627bb02d21324e4ec2f4ba62921088c2b082988238325beb2",
	"119_cloud_resource_retract_liveness_stats.sql":                          "694473c935571008ca27b3891a81b283550b5b21f4f542a8f1e5d55212b9c9c7",
	"120_value_flow_refresh_code_function_summary_producer.sql":              "a5c555ba32b1f2b2e8f873a41175f739db2d5e18a44dc662638c60ba57cd6cbb",
	"121_fact_records_content_entity_dependency_variable_repo_idx.sql":       "25d345d5212ac7951e45a4e3e2a447f8a5665f8a0743085a8697ec54c94d1504",
	"122_fact_records_documentation_source_only_idx.sql":                     "2cf12a55d9dc670509d6705e87580fd0a3c3743e9f11323e08cc3867352e3527",
	"122_service_materialization_generations_scope_column.sql":               "5eed3c50ec32e6bb8bf6932af3e45f8fe5b322d740b4a1fad0ba1aca3665e4ee",
	"123_fact_records_story_support_kinds_idx.sql":                           "61781d5dba75ac28a370df8f106dd5bbf96cce2b6f22e64897636af82eb0aeb8",
	"123_service_materialization_generations_scope_backfill.sql":             "a631a962c21e9329add53819780c678f8b3bb7ac6b0e34b2aaa25b31435cb5bf",
	"124_fact_records_documentation_semantic_target_refs_idx.sql":            "78b76f88b0983ae492bb4a75c34a3db3081611a0147a6d8e5abdeae03a98b0c0",
	"124_service_materialization_generations_active_service_idx_rescope.sql": "9be3eabff082f477e045af034482f8995bd95ebfe5c0a460064e403cf0f37a47",
	"125_service_materialization_generations_active_service_idx_v2.sql":      "5b8f1f36141d248b7eb946fe8d6b9890a95c2e8ca3b72eec18ba47f7ffb46b20",
	"125_shared_projection_acceptance_generation_key.sql":                    "9f2be191577597e10b8368e93fa236e376938f74d1515efa92f5f4c081341152",
}

// validateMigrationManifest checks defs against manifest: every definition
// must be pinned with a matching checksum, and every pinned entry must still
// have a definition (a deletion is refused just like an edit).
func validateMigrationManifest(defs []Definition, manifest map[string]string) error {
	seen := make(map[string]bool, len(manifest))
	for _, def := range defs {
		name := path.Base(def.Path)
		want, ok := manifest[name]
		if !ok {
			return fmt.Errorf("migration %q is not in the checksum manifest -- #7002: append its shipped checksum instead of leaving it unpinned", name)
		}
		seen[name] = true
		if got := Checksum(def.SQL); got != want {
			return fmt.Errorf("migration %q bytes changed: manifest %s, current %s -- #7002: a shipped migration must never be edited; widen it through a new guarded migration instead",
				name, want, got)
		}
	}
	for name := range manifest {
		if !seen[name] {
			return fmt.Errorf("migration %q is pinned in the checksum manifest but missing from BootstrapDefinitions() -- #7002: a shipped migration must never be deleted", name)
		}
	}
	return nil
}

func TestMigrationChecksumManifestPinsEveryShippedMigration(t *testing.T) {
	if err := validateMigrationManifest(BootstrapDefinitions(), migrationShippedChecksums); err != nil {
		t.Fatal(err)
	}
}

// TestMigrationChecksumManifestDetectsTamperedFile is the seeded-violation
// RED half of the #7002 immutability guard: it plants a one-byte edit in
// memory (no file on disk is touched) and requires the failure to name the
// tampered file.
func TestMigrationChecksumManifestDetectsTamperedFile(t *testing.T) {
	defs := append([]Definition(nil), BootstrapDefinitions()...)
	tampered := defs[0]
	tampered.SQL += "-- #7002 seeded violation"
	defs[0] = tampered

	err := validateMigrationManifest(defs, migrationShippedChecksums)
	if err == nil {
		t.Fatal("validateMigrationManifest() = nil, want an error for a tampered migration")
	}
	if !strings.Contains(err.Error(), path.Base(tampered.Path)) {
		t.Fatalf("error %v does not name the tampered file %q", err, tampered.Path)
	}
	if !strings.Contains(err.Error(), "7002") {
		t.Fatalf("error %v does not point at #7002", err)
	}
}

// TestMigrationChecksumManifestDetectsDeletedFile is the deletion half of the
// guard: removing a shipped migration from the definitions list must fail
// loud, naming the missing file, rather than silently shrinking the applied
// set.
func TestMigrationChecksumManifestDetectsDeletedFile(t *testing.T) {
	defs := BootstrapDefinitions()
	removed := defs[0]
	without := append([]Definition(nil), defs[1:]...)

	err := validateMigrationManifest(without, migrationShippedChecksums)
	if err == nil {
		t.Fatal("validateMigrationManifest() = nil, want an error for a deleted migration")
	}
	if !strings.Contains(err.Error(), path.Base(removed.Path)) {
		t.Fatalf("error %v does not name the deleted file %q", err, removed.Path)
	}
}
