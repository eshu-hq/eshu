resource "aws_route53_record" "service_edge_api_legacy" {
  zone_id = "ZTEST123"
  name    = "api.example.test"
  type    = "CNAME"
  ttl     = 60
  records = ["legacy-edge.example.test"]
}

resource "cloudflare_record" "service_edge_api_modern" {
  zone_id = "zone123"
  name    = "api-modern.internal.test"
  type    = "CNAME"
  value   = "modern-edge.internal.test"
  proxied = true
}
