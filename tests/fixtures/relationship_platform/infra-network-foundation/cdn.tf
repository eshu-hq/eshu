resource "aws_cloudfront_distribution" "service_edge_api_public" {
  enabled = true
  aliases = ["api.example.test"]

  origin {
    domain_name = "legacy-edge.example.test"
    origin_id   = "service-edge-api-origin"
  }

  default_cache_behavior {
    target_origin_id       = "service-edge-api-origin"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
