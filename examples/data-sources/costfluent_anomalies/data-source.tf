# Anomalies nobody has acknowledged yet
data "costfluent_anomalies" "open" {
  unacknowledged_only = true
  limit               = 50
}

output "open_anomaly_count" {
  value = data.costfluent_anomalies.open.unacknowledged_count
}

output "high_severity" {
  value = [for a in data.costfluent_anomalies.open.anomalies : a if a.severity == "high"]
}
