plugins {
    `java-library`
    id("io.spring.dependency-management")
}

dependencyManagement {
    imports { mavenBom("org.springframework.boot:spring-boot-dependencies:4.1.2") }
}

dependencies {
    api("org.springframework.boot:spring-boot-starter-webmvc")
    api("org.springframework.boot:spring-boot-starter-jdbc")
    implementation("org.springframework.boot:spring-boot-autoconfigure")
    testImplementation("org.springframework.boot:spring-boot-starter-test")
}
