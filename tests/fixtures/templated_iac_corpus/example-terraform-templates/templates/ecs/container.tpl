[
  {
    "name": "${name}",
    "image": "${image}",
    "cpu": ${cpu},
    "memoryReservation": ${memory},
    "essential": true,
    "environment": ${environment},
    "portMappings": [
      {
        "containerPort": ${port},
        "hostPort": 0,
        "protocol": "tcp"
      }
    ],
    "logConfiguration": {
      "logDriver": "awslogs",
      "options": {
        "awslogs-group": "${log_group}",
        "awslogs-region": "${log_region}",
        "awslogs-stream-prefix": "${log_prefix}"
      }
    },
    "mountPoints":[],
    "volumesFrom":[]
  }
]