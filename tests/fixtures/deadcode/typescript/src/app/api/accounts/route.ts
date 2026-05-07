export async function GET() {
  return Response.json({ ok: true });
}

function localRouteHelper() {
  return "not a root";
}

void localRouteHelper;
