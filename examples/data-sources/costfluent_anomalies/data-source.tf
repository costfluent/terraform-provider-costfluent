# Anomalies nobody has acknowledged yet
data "costfluent_anomalies" "open" {
  unacknowledged_only = true
  limit               = 50
}

output "open_anomaly_count" {
  value = length(data.costfluent_anomalies.open.anomalies)
}

output "high_severity" {
  value = [for a in data.costfluent_anomalies.open.anomalies : a if a.severity == "high"]
}
