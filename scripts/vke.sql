-- MySQL dump 10.13  Distrib 8.0.36, for Linux (x86_64)
--
-- Host: localhost    Database: vke
-- ------------------------------------------------------
-- Server version	8.0.36-0ubuntu0.20.04.1

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Table structure for table `audit_log`
--

DROP TABLE IF EXISTS `audit_log`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `audit_log` (
  `id` int NOT NULL AUTO_INCREMENT,
  `project_uuid` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `event` text,
  `create_date` datetime DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=1157 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `clusters`
--

DROP TABLE IF EXISTS `clusters`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `clusters` (
  `id` int NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `cluster_name` varchar(50) DEFAULT NULL,
  `cluster_create_date` datetime DEFAULT NULL,
  `cluster_delete_date` datetime DEFAULT NULL,
  `cluster_update_date` datetime DEFAULT NULL,
  `cluster_version` varchar(30) DEFAULT NULL,
  `cluster_status` enum('Active','Creating','Updating','Deleting','Deleted','Error') DEFAULT NULL,
  `cluster_project_uuid` varchar(255) DEFAULT NULL,
  `cluster_loadbalancer_uuid` varchar(255) DEFAULT NULL,
  `cluster_register_token` varchar(255) DEFAULT NULL,
  `cluster_subnets` json DEFAULT NULL,
  `cluster_node_keypair_name` varchar(140) DEFAULT NULL,
  `cluster_endpoint` varchar(144) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL,
  `cluster_api_access` enum('public','private') DEFAULT 'public',
  `cluster_agent_token` varchar(255) DEFAULT NULL,
  `floating_ip_uuid` varchar(255) DEFAULT NULL,
  `cluster_cloudflare_record_id` varchar(36) DEFAULT NULL,
  `cluster_shared_security_group` varchar(50) DEFAULT NULL,
  `application_credential_id` varchar(36) DEFAULT NULL,
  `delete_state` enum('initial', 'loadbalancer', 'dns', 'floating_ip', 'nodes', 'security_groups', 'credentials', 'completed') DEFAULT 'initial',
  `cluster_certificate_expire_date` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `cluster_uuid` (`cluster_uuid`)
) ENGINE=InnoDB AUTO_INCREMENT=78 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `errors`
--

DROP TABLE IF EXISTS `errors`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `errors` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `error_message` text NOT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_cluster_uuid` (`cluster_uuid`),
  KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `kubeconfigs`
--

DROP TABLE IF EXISTS `kubeconfigs`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `kubeconfigs` (
  `id` int NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `kubeconfig` text,
  `create_date` datetime DEFAULT NULL,
  `update_date` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `cluster_uuid` (`cluster_uuid`),
  CONSTRAINT `kubeconfigs_ibfk_1` FOREIGN KEY (`cluster_uuid`) REFERENCES `clusters` (`cluster_uuid`)
) ENGINE=InnoDB AUTO_INCREMENT=26 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `node_groups`
--

DROP TABLE IF EXISTS `node_groups`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `node_groups` (
  `id` int NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `node_group_name` varchar(255) DEFAULT NULL,
  `node_group_labels` json DEFAULT NULL,
  `node_group_taints` json DEFAULT NULL,
  `node_group_uuid` varchar(36) DEFAULT NULL,
  `node_group_min_size` int DEFAULT NULL,
  `node_group_max_size` int DEFAULT NULL,
  `node_disk_size` int DEFAULT NULL,
  `node_flavor_uuid` varchar(36) DEFAULT NULL,
  `node_groups_status` enum('Active','Updating','Deleted','Creating') DEFAULT NULL,
  `node_groups_type` enum('master','worker') DEFAULT NULL,
  `node_group_security_group` varchar(50) DEFAULT NULL,
  `is_hidden` tinyint(1) DEFAULT NULL,
  `node_group_create_date` datetime DEFAULT NULL,
  `node_group_update_date` datetime DEFAULT NULL,
  `node_group_delete_date` datetime DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=77 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `resources`
--

DROP TABLE IF EXISTS `resources`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `resources` (
  `id` int NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `resource_type` varchar(30) DEFAULT NULL,
  `resource_uuid` varchar(36) DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=77 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2024-04-17 10:25:39
