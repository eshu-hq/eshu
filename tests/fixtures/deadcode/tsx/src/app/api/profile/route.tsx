export const POST = async () => {
  return Response.json({ ok: true });
};

const localRouteHelper = () => Response.json({ ok: true });

void localRouteHelper;
