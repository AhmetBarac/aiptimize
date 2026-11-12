package io.aiptimize.companion

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import io.aiptimize.companion.ui.AddServerScreen
import io.aiptimize.companion.ui.AiptimizeTheme
import io.aiptimize.companion.ui.MainViewModel
import io.aiptimize.companion.ui.ServerSettingsScreen
import io.aiptimize.companion.ui.ServersScreen
import io.aiptimize.companion.ui.ViewerScreen

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            AiptimizeTheme {
                val nav = rememberNavController()
                val vm: MainViewModel = viewModel()
                val servers by vm.servers.collectAsState()
                val discovered by vm.discovered.collectAsState()
                val scanning by vm.scanning.collectAsState()

                NavHost(navController = nav, startDestination = "servers") {
                    composable("servers") {
                        ServersScreen(
                            servers = servers,
                            onAdd = { nav.navigate("add") },
                            onOpen = { nav.navigate("viewer/${it.instanceId}") },
                            onSettings = { nav.navigate("settings/${it.instanceId}") },
                        )
                    }
                    composable("add") {
                        AddServerScreen(
                            vm = vm,
                            scanning = scanning,
                            discovered = discovered,
                            onScan = vm::scan,
                            onSave = { profile ->
                                vm.save(profile)
                                nav.popBackStack("servers", inclusive = false)
                            },
                            onBack = { nav.popBackStack() },
                        )
                    }
                    composable(
                        route = "viewer/{id}",
                        arguments = listOf(navArgument("id") { type = NavType.StringType }),
                    ) { entry ->
                        ViewerScreen(
                            vm = vm,
                            instanceId = entry.arguments?.getString("id").orEmpty(),
                            onBack = { nav.popBackStack() },
                        )
                    }
                    composable(
                        route = "settings/{id}",
                        arguments = listOf(navArgument("id") { type = NavType.StringType }),
                    ) { entry ->
                        val id = entry.arguments?.getString("id").orEmpty()
                        ServerSettingsScreen(
                            profile = servers.firstOrNull { it.instanceId == id },
                            onSave = { profile ->
                                vm.save(profile)
                                nav.popBackStack()
                            },
                            onRemove = { profile ->
                                vm.remove(profile.instanceId)
                                nav.popBackStack("servers", inclusive = false)
                            },
                            onBack = { nav.popBackStack() },
                        )
                    }
                }
            }
        }
    }
}
