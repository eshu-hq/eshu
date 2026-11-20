import sys
hb_before, hb_after, n, h = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
with open(hb_before, "w") as f:
    f.write("\\timing off\n")
    for _ in range(h):
        for g in range(1, n + 1):
            wid = f"'w{g:07d}'"
            f.write(
                "UPDATE q_before SET claim_until = clock_timestamp() + interval '5 minutes',"
                f" updated_at = clock_timestamp() WHERE work_item_id = {wid};\n"
            )
with open(hb_after, "w") as f:
    f.write("\\timing off\n")
    for _ in range(h):
        for g in range(1, n + 1):
            wid = f"'w{g:07d}'"
            f.write(
                "UPDATE q_lease SET claim_until = clock_timestamp() + interval '5 minutes'"
                f" WHERE work_item_id = {wid};\n"
            )
print(f"generated {n * h} renewals per shape")
